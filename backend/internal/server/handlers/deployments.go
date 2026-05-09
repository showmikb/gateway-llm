package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
)

type createDeploymentRequest struct {
	ModelAlias      string     `json:"model_alias"`
	Provider        string     `json:"provider"`
	ProviderModel   string     `json:"provider_model"`
	APIKeyEnv       string     `json:"api_key_env,omitempty"`
	CredentialID    *uuid.UUID `json:"credential_id,omitempty"`
	OrgID           *uuid.UUID `json:"org_id,omitempty"`
	APIBase         string     `json:"api_base,omitempty"`
	Priority        int        `json:"priority"`
	RoutingStrategy string     `json:"routing_strategy,omitempty"`
	Weight          int        `json:"weight,omitempty"`
}

type updateDeploymentRequest struct {
	ModelAlias      string     `json:"model_alias,omitempty"`
	Provider        string     `json:"provider,omitempty"`
	ProviderModel   string     `json:"provider_model,omitempty"`
	APIKeyEnv       string     `json:"api_key_env,omitempty"`
	CredentialID    *uuid.UUID `json:"credential_id,omitempty"`
	OrgID           *uuid.UUID `json:"org_id,omitempty"`
	APIBase         string     `json:"api_base,omitempty"`
	Priority        *int       `json:"priority,omitempty"`
	IsActive        *bool      `json:"is_active,omitempty"`
	RoutingStrategy string     `json:"routing_strategy,omitempty"`
	Weight          *int       `json:"weight,omitempty"`
}

type updateAliasStrategyRequest struct {
	ModelAlias      string     `json:"model_alias"`
	RoutingStrategy string     `json:"routing_strategy"`
	OrgID           *uuid.UUID `json:"org_id,omitempty"`
}

func validRoutingStrategy(s string) bool {
	switch s {
	case "round-robin", "least-latency", "priority", "cheapest", "weighted":
		return true
	}
	return false
}

func (h *Handlers) CreateDeploymentHandler(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in createDeploymentRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.ModelAlias == "" || in.Provider == "" || in.ProviderModel == "" {
		writeErrorResp(w, http.StatusBadRequest, "model_alias, provider, and provider_model are required")
		return
	}

	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		if callerOrg == nil {
			writeErrorResp(w, http.StatusForbidden, "cannot determine caller organization")
			return
		}
		if in.OrgID != nil && *in.OrgID != *callerOrg {
			writeErrorResp(w, http.StatusForbidden, "cannot create deployments outside your organization")
			return
		}
		in.OrgID = callerOrg
	}

	if in.CredentialID != nil && in.OrgID != nil {
		cred, err := h.DB.GetCredential(r.Context(), *in.CredentialID)
		if err != nil {
			writeErrorResp(w, http.StatusBadRequest, "credential not found")
			return
		}
		if cred.OrgID != nil && *cred.OrgID != *in.OrgID {
			writeErrorResp(w, http.StatusBadRequest, "credential org_id does not match deployment org_id")
			return
		}
	}

	strategy := in.RoutingStrategy
	if strategy == "" {
		strategy = "round-robin"
	}
	if !validRoutingStrategy(strategy) {
		writeErrorResp(w, http.StatusBadRequest, "invalid routing_strategy")
		return
	}

	if existing, lerr := h.DB.ListDeployments(r.Context()); lerr == nil {
		for _, d := range existing {
			if d.ModelAlias != in.ModelAlias {
				continue
			}
			sameOrg := (d.OrgID == nil && in.OrgID == nil) ||
				(d.OrgID != nil && in.OrgID != nil && *d.OrgID == *in.OrgID)
			if !sameOrg {
				continue
			}
			if d.RoutingStrategy != "" && d.RoutingStrategy != strategy {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error": map[string]any{
						"message":           "existing targets for this alias use a different routing strategy",
						"type":              "strategy_mismatch",
						"current_strategy":  d.RoutingStrategy,
						"requested":         strategy,
						"model_alias":       in.ModelAlias,
					},
				})
				return
			}
		}
	}

	weight := in.Weight
	if weight <= 0 {
		weight = 1
	}

	dep := &models.Deployment{
		ModelAlias:      in.ModelAlias,
		Provider:        in.Provider,
		ProviderModel:   in.ProviderModel,
		APIKeyEnv:       in.APIKeyEnv,
		CredentialID:    in.CredentialID,
		OrgID:           in.OrgID,
		APIBase:         in.APIBase,
		Priority:        in.Priority,
		Weight:          weight,
		RoutingStrategy: strategy,
		IsActive:        true,
	}
	if err := h.DB.CreateDeployment(r.Context(), dep); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.triggerRouterReload(r.Context())
	writeJSON(w, http.StatusCreated, dep)
}

func (h *Handlers) UpdateDeploymentHandler(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}

	var in updateDeploymentRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	deps, err := h.DB.ListDeployments(r.Context())
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	var found *models.Deployment
	for i := range deps {
		if deps[i].ID == id {
			found = &deps[i]
			break
		}
	}
	if found == nil {
		writeErrorResp(w, http.StatusNotFound, "deployment not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), found.OrgID) {
		writeErrorResp(w, http.StatusNotFound, "deployment not found")
		return
	}

	if in.ModelAlias != "" {
		found.ModelAlias = in.ModelAlias
	}
	if in.Provider != "" {
		found.Provider = in.Provider
	}
	if in.ProviderModel != "" {
		found.ProviderModel = in.ProviderModel
	}
	if in.APIKeyEnv != "" {
		found.APIKeyEnv = in.APIKeyEnv
	}
	if in.CredentialID != nil {
		found.CredentialID = in.CredentialID
	}
	if in.OrgID != nil {
		found.OrgID = in.OrgID
	}
	if in.APIBase != "" {
		found.APIBase = in.APIBase
	}
	if in.Priority != nil {
		found.Priority = *in.Priority
	}
	if in.IsActive != nil {
		found.IsActive = *in.IsActive
	}
	if in.RoutingStrategy != "" {
		if !validRoutingStrategy(in.RoutingStrategy) {
			writeErrorResp(w, http.StatusBadRequest, "invalid routing_strategy")
			return
		}
		found.RoutingStrategy = in.RoutingStrategy
	}
	if in.Weight != nil {
		if *in.Weight <= 0 {
			found.Weight = 1
		} else {
			found.Weight = *in.Weight
		}
	}

	if found.CredentialID != nil && found.OrgID != nil {
		cred, err := h.DB.GetCredential(r.Context(), *found.CredentialID)
		if err == nil && cred != nil && cred.OrgID != nil && *cred.OrgID != *found.OrgID {
			writeErrorResp(w, http.StatusBadRequest, "credential org_id does not match deployment org_id")
			return
		}
	}

	if err := h.DB.UpdateDeployment(r.Context(), found); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.triggerRouterReload(r.Context())
	writeJSON(w, http.StatusOK, found)
}

func (h *Handlers) DeleteDeploymentHandler(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !middleware.IsSuperAdmin(r.Context()) {
		deps, lerr := h.DB.ListDeployments(r.Context())
		if lerr != nil {
			writeErrorResp(w, http.StatusInternalServerError, lerr.Error())
			return
		}
		var found *models.Deployment
		for i := range deps {
			if deps[i].ID == id {
				found = &deps[i]
				break
			}
		}
		if found == nil || !middleware.CanAccessOrg(r.Context(), found.OrgID) {
			writeErrorResp(w, http.StatusNotFound, "deployment not found")
			return
		}
	}
	if err := h.DB.DeleteDeployment(r.Context(), id); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.triggerRouterReload(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// UpdateAliasStrategyHandler bulk-updates routing_strategy for all deployments in a
// (org, model_alias) group. Callers can specify org_id or rely on their caller org.
func (h *Handlers) UpdateAliasStrategyHandler(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in updateAliasStrategyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.ModelAlias == "" || in.RoutingStrategy == "" {
		writeErrorResp(w, http.StatusBadRequest, "model_alias and routing_strategy are required")
		return
	}
	if !validRoutingStrategy(in.RoutingStrategy) {
		writeErrorResp(w, http.StatusBadRequest, "invalid routing_strategy")
		return
	}

	orgID := in.OrgID
	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		if callerOrg == nil {
			writeErrorResp(w, http.StatusForbidden, "cannot determine caller organization")
			return
		}
		if orgID != nil && *orgID != *callerOrg {
			writeErrorResp(w, http.StatusForbidden, "cannot update deployments outside your organization")
			return
		}
		orgID = callerOrg
	}

	if err := h.DB.UpdateAliasStrategy(r.Context(), orgID, in.ModelAlias, in.RoutingStrategy); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.triggerRouterReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"model_alias":      in.ModelAlias,
		"routing_strategy": in.RoutingStrategy,
	})
}
