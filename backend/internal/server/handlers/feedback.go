package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
)

// feedbackRequest is the body accepted by POST /v1/feedback. Callers
// attach a quality signal to a previously-served inference via its
// trace_id (returned in the X-Gateway-LLM-Trace-ID response header).
type feedbackRequest struct {
	TraceID    string   `json:"trace_id"`
	Metric     string   `json:"metric"`
	ValueFloat *float64 `json:"value_float,omitempty"`
	ValueBool  *bool    `json:"value_bool,omitempty"`
	ValueText  string   `json:"value_text,omitempty"`
	ModelAlias string   `json:"model_alias,omitempty"`
	VariantID  string   `json:"variant_id,omitempty"`
}

// CreateFeedback persists a quality signal. A caller SHOULD provide at
// least one of value_float / value_bool / value_text. Returns 201 with
// the stored event.
func (h *Handlers) CreateFeedback(w http.ResponseWriter, r *http.Request) {
	var in feedbackRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.TraceID == "" {
		writeErrorResp(w, http.StatusBadRequest, "trace_id is required")
		return
	}
	if in.Metric == "" {
		writeErrorResp(w, http.StatusBadRequest, "metric is required")
		return
	}
	if in.ValueFloat == nil && in.ValueBool == nil && in.ValueText == "" {
		writeErrorResp(w, http.StatusBadRequest, "one of value_float, value_bool, or value_text is required")
		return
	}

	ev := &models.FeedbackEvent{
		TraceID:    in.TraceID,
		Metric:     in.Metric,
		ValueFloat: in.ValueFloat,
		ValueBool:  in.ValueBool,
		ValueText:  in.ValueText,
		ModelAlias: in.ModelAlias,
		VariantID:  in.VariantID,
	}
	if key := middleware.GetAPIKey(r.Context()); key != nil && key.ID != uuid.Nil {
		kid := key.ID
		ev.APIKeyID = &kid
	}
	if orgID := middleware.GetOrgID(r.Context()); orgID != nil {
		ev.OrgID = orgID
	}

	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	if err := h.DB.InsertFeedback(r.Context(), ev); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ev)
}

// GetMetrics returns aggregated quality signals over the last N days
// (default 7, max 90), scoped to the caller's org when applicable.
func (h *Handlers) GetMetrics(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	if h.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"data":           []any{},
			"smart_route":    smartStats(h),
			"semantic_cache": semStats(h),
		})
		return
	}
	var orgID *uuid.UUID
	if !middleware.IsSuperAdmin(r.Context()) {
		orgID = middleware.GetOrgID(r.Context())
	}
	metrics, err := h.DB.AggregateFeedback(r.Context(), orgID, days)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"days":           days,
		"data":           metrics,
		"smart_route":    smartStats(h),
		"semantic_cache": semStats(h),
	})
}

func semStats(h *Handlers) map[string]any {
	if h.SemCache == nil || !h.SemCache.Enabled() {
		return map[string]any{"enabled": false}
	}
	hits, misses := h.SemCache.Stats()
	return map[string]any{"enabled": true, "hits": hits, "misses": misses}
}

func smartStats(h *Handlers) map[string]any {
	if h.SmartRoute == nil {
		return map[string]any{"enabled": false}
	}
	decisions, overrides := h.SmartRoute.Stats()
	out := map[string]any{
		"enabled":         true,
		"total_decisions": decisions,
		"tier_overrides":  overrides,
	}
	if hasModel, trainedAt, samples := h.SmartRoute.ModelInfo(); hasModel {
		out["ml_model"] = map[string]any{
			"loaded":     true,
			"trained_at": trainedAt,
			"samples":    samples,
		}
	}
	if runner := h.SmartRoute.Shadow(); runner != nil {
		out["shadow"] = runner.Stats()
	}
	return out
}

// ListAuditEvents returns the most recent audit-log entries for the
// caller's org (or all orgs for super admins). The list is read-only;
// the API deliberately offers no mutations for compliance.
func (h *Handlers) ListAuditEvents(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	var orgID *uuid.UUID
	if !middleware.IsSuperAdmin(r.Context()) {
		orgID = middleware.GetOrgID(r.Context())
	}
	events, err := h.DB.ListAuditEvents(r.Context(), orgID, limit)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": events})
}
