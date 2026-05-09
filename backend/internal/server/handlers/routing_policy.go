package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
)

// ListRoutingPolicies returns the per-alias routing policy table the
// /routing UI renders. Org admins see their org's overrides + the
// global defaults; master sees everything.
func (h *Handlers) ListRoutingPolicies(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	var orgID *uuid.UUID
	if !middleware.IsMaster(r.Context()) && !middleware.IsSuperAdmin(r.Context()) {
		orgID = middleware.GetOrgID(r.Context())
	} else if v := r.URL.Query().Get("org_id"); v != "" {
		oid, err := uuid.Parse(v)
		if err == nil {
			orgID = &oid
		}
	}
	rows, err := h.DB.ListRoutingPolicies(r.Context(), orgID)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

type routingPolicyPayload struct {
	OrgID                   *string  `json:"org_id,omitempty"`
	Strategy                string   `json:"strategy"`
	QualityThreshold        float64  `json:"quality_threshold"`
	BaselineProviderModel   *string  `json:"baseline_provider_model,omitempty"`
	JudgeAlias              *string  `json:"judge_alias,omitempty"`
	RetryWhenStreaming      bool     `json:"retry_when_streaming"`
	SamplePct               int      `json:"sample_pct"`
	MinSamplesBeforeRouting int      `json:"min_samples_before_routing"`
}

// UpsertRoutingPolicy: org admin or master. Org admins can only edit
// their own org's policy (or pass org_id="" / null to create a global
// default if they're master).
func (h *Handlers) UpsertRoutingPolicy(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	alias := strings.TrimSpace(chi.URLParam(r, "alias"))
	if alias == "" {
		writeErrorResp(w, http.StatusBadRequest, "alias is required")
		return
	}
	var body routingPolicyPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<19)).Decode(&body); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Strategy == "" {
		body.Strategy = "track_only"
	}
	switch body.Strategy {
	case "off", "track_only", "auto_retry", "judge_then_decide", "shadow_learn":
	default:
		writeErrorResp(w, http.StatusBadRequest, "strategy must be one of off/track_only/auto_retry/judge_then_decide/shadow_learn")
		return
	}
	if body.QualityThreshold < 0 || body.QualityThreshold > 1 {
		writeErrorResp(w, http.StatusBadRequest, "quality_threshold must be in [0,1]")
		return
	}
	if body.SamplePct < 0 || body.SamplePct > 100 {
		writeErrorResp(w, http.StatusBadRequest, "sample_pct must be in [0,100]")
		return
	}
	p := &models.RoutingPolicy{
		ModelAlias:              alias,
		Strategy:                body.Strategy,
		QualityThreshold:        body.QualityThreshold,
		BaselineProviderModel:   body.BaselineProviderModel,
		JudgeAlias:              body.JudgeAlias,
		RetryWhenStreaming:      body.RetryWhenStreaming,
		SamplePct:               body.SamplePct,
		MinSamplesBeforeRouting: body.MinSamplesBeforeRouting,
	}
	if middleware.IsMaster(r.Context()) || middleware.IsSuperAdmin(r.Context()) {
		if body.OrgID != nil && *body.OrgID != "" {
			oid, err := uuid.Parse(*body.OrgID)
			if err != nil {
				writeErrorResp(w, http.StatusBadRequest, "invalid org_id")
				return
			}
			p.OrgID = &oid
		}
	} else {
		oid := middleware.GetOrgID(r.Context())
		if oid == nil {
			writeErrorResp(w, http.StatusForbidden, "org context required")
			return
		}
		p.OrgID = oid
	}
	if err := h.DB.UpsertRoutingPolicy(r.Context(), p); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// DeleteRoutingPolicy removes the per-alias policy. Caller must own
// the org (org admin) or be master.
func (h *Handlers) DeleteRoutingPolicy(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	alias := strings.TrimSpace(chi.URLParam(r, "alias"))
	if alias == "" {
		writeErrorResp(w, http.StatusBadRequest, "alias required")
		return
	}
	var orgID *uuid.UUID
	if !middleware.IsMaster(r.Context()) && !middleware.IsSuperAdmin(r.Context()) {
		orgID = middleware.GetOrgID(r.Context())
	}
	existing, err := h.DB.GetRoutingPolicy(r.Context(), alias, orgID)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeErrorResp(w, http.StatusNotFound, "routing policy not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), existing.OrgID) {
		writeErrorResp(w, http.StatusForbidden, "cannot delete policy in another org")
		return
	}
	if err := h.DB.DeleteRoutingPolicy(r.Context(), existing.ID); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
