package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/crypto"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/providers"
	"go.uber.org/zap"
)

type discoverRequest struct {
	CredentialID string `json:"credential_id"`
}

type syncRequest struct {
	CredentialID string     `json:"credential_id"`
	OrgID        *uuid.UUID `json:"org_id,omitempty"`
	Models       []string   `json:"models"`
	SkipExisting bool       `json:"skip_existing"`
}

func (h *Handlers) DiscoverModels(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in discoverRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	credID, err := uuid.Parse(in.CredentialID)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid credential_id")
		return
	}

	cred, err := h.DB.GetCredential(r.Context(), credID)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "credential not found")
		return
	}

	apiKey, err := crypto.Decrypt(cred.APIKeyEnc)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to decrypt credential")
		return
	}

	prov, err2 := h.Registry.Get(cred.Provider)
	if err2 != nil {
		writeErrorResp(w, http.StatusBadRequest, "unknown provider: "+cred.Provider)
		return
	}

	discoverer, ok := prov.(providers.ModelDiscoveryProvider)
	if !ok {
		writeErrorResp(w, http.StatusBadRequest, "provider does not support model discovery")
		return
	}

	discovered, err := discoverer.DiscoverModels(r.Context(), apiKey, cred.APIBase)
	if err != nil {
		h.Logger.Warn("model discovery failed", zap.Error(err), zap.String("provider", cred.Provider))
		writeErrorResp(w, http.StatusBadGateway, "discovery failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": discovered})
}

func (h *Handlers) SyncRoutes(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in syncRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(in.Models) == 0 {
		writeErrorResp(w, http.StatusBadRequest, "models list is required")
		return
	}

	credID, err := uuid.Parse(in.CredentialID)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid credential_id")
		return
	}

	cred, err := h.DB.GetCredential(r.Context(), credID)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "credential not found")
		return
	}

	created := 0
	skipped := 0
	var routes []models.Deployment

	for _, modelID := range in.Models {
		if in.SkipExisting {
			exists, err := h.DB.DeploymentExistsByAliasProviderOrg(r.Context(), modelID, cred.Provider, in.OrgID)
			if err != nil {
				h.Logger.Warn("check existing deployment", zap.Error(err))
			}
			if exists {
				skipped++
				continue
			}
		}

		dep := &models.Deployment{
			ModelAlias:    modelID,
			Provider:      cred.Provider,
			ProviderModel: modelID,
			CredentialID:  &credID,
			OrgID:         in.OrgID,
			Priority:      0,
			IsActive:      true,
		}
		if err := h.DB.CreateDeployment(r.Context(), dep); err != nil {
			h.Logger.Warn("create synced deployment", zap.Error(err), zap.String("model", modelID))
			skipped++
			continue
		}
		routes = append(routes, *dep)
		created++
	}

	h.triggerRouterReload(r.Context())

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"created": created,
		"skipped": skipped,
		"routes":  routes,
	})
}
