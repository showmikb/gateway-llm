package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/crypto"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"go.uber.org/zap"
)

func (h *Handlers) triggerRouterReload(ctx context.Context) {
	if h.DB == nil || h.Router == nil {
		return
	}
	deps, err := h.DB.GetActiveDeployments(ctx)
	if err != nil {
		h.Logger.Warn("reload router: fetch deployments", zap.Error(err))
		return
	}

	aliases := make(map[string][]*router.DeploymentInfo)
	for _, d := range deps {
		apiKey := ""
		if d.CredentialID != nil {
			cred, err := h.DB.GetCredential(ctx, *d.CredentialID)
			if err == nil && cred != nil && cred.IsActive {
				decrypted, err := crypto.Decrypt(cred.APIKeyEnc)
				if err == nil {
					apiKey = decrypted
				}
			}
		}
		if apiKey == "" && d.APIKeyEnv != "" {
			apiKey = os.Getenv(d.APIKeyEnv)
		}

		aliases[d.ModelAlias] = append(aliases[d.ModelAlias], &router.DeploymentInfo{
			Provider:      d.Provider,
			ProviderModel: d.ProviderModel,
			APIKey:        apiKey,
			APIBase:       d.APIBase,
			Priority:      d.Priority,
			OrgID:         d.OrgID,
		})
	}

	h.Router.Reload(aliases)
	h.Logger.Info("router hot-reloaded", zap.Int("aliases", len(aliases)))
}

type updateSettingsRequest struct {
	Routing      *routingSettings    `json:"routing,omitempty"`
	RateLimiting *rateLimitSettings  `json:"rate_limiting,omitempty"`
	Logging      *loggingSettings    `json:"logging,omitempty"`
}

type routingSettings struct {
	Strategy        string `json:"strategy,omitempty"`
	Retries         *int   `json:"retries,omitempty"`
	RetryDelayMS    *int64 `json:"retry_delay_ms,omitempty"`
	FallbackEnabled *bool  `json:"fallback_enabled,omitempty"`
}

type rateLimitSettings struct {
	DefaultRPM *int `json:"default_rpm,omitempty"`
	DefaultTPM *int `json:"default_tpm,omitempty"`
}

type loggingSettings struct {
	Level  string `json:"level,omitempty"`
	Format string `json:"format,omitempty"`
}

func (h *Handlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}

	var in updateSettingsRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx := r.Context()
	if in.Routing != nil {
		data, _ := json.Marshal(in.Routing)
		if err := h.DB.UpsertSetting(ctx, "routing", data); err != nil {
			h.Logger.Error("upsert routing setting", zap.Error(err))
			writeErrorResp(w, http.StatusInternalServerError, "failed to update routing settings")
			return
		}
	}
	if in.RateLimiting != nil {
		data, _ := json.Marshal(in.RateLimiting)
		if err := h.DB.UpsertSetting(ctx, "rate_limiting", data); err != nil {
			h.Logger.Error("upsert rate_limiting setting", zap.Error(err))
			writeErrorResp(w, http.StatusInternalServerError, "failed to update rate limiting settings")
			return
		}
	}
	if in.Logging != nil {
		data, _ := json.Marshal(in.Logging)
		if err := h.DB.UpsertSetting(ctx, "logging", data); err != nil {
			h.Logger.Error("upsert logging setting", zap.Error(err))
			writeErrorResp(w, http.StatusInternalServerError, "failed to update logging settings")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

type updateAPIKeyRequest struct {
	Name      string     `json:"name,omitempty"`
	TeamID    *uuid.UUID `json:"team_id,omitempty"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	Models    []string   `json:"models,omitempty"`
	RPMLimit  *int       `json:"rpm_limit,omitempty"`
	TPMLimit  *int       `json:"tpm_limit,omitempty"`
	MaxBudget *float64   `json:"max_budget,omitempty"`
	IsActive  *bool      `json:"is_active,omitempty"`
}

func (h *Handlers) UpdateAPIKeyHandler(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}

	var keys []models.APIKey
	if middleware.HasRole(r.Context(), middleware.RoleOrgAdmin) || middleware.IsMaster(r.Context()) {
		keys, err = h.DB.ListAPIKeys(r.Context())
	} else if user := middleware.GetUser(r.Context()); user != nil {
		keys, err = h.DB.ListAPIKeysForUser(r.Context(), user.ID)
	} else {
		writeErrorResp(w, http.StatusForbidden, "access denied")
		return
	}
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to look up keys")
		return
	}
	var found *models.APIKey
	for i := range keys {
		if keys[i].ID == id {
			found = &keys[i]
			break
		}
	}
	if found == nil {
		writeErrorResp(w, http.StatusNotFound, "key not found")
		return
	}
	// Make sure the key's team belongs to the caller's org unless caller is super admin.
	if !middleware.IsSuperAdmin(r.Context()) && found.TeamID != nil {
		team, terr := h.DB.GetTeam(r.Context(), *found.TeamID)
		if terr != nil || !middleware.CanAccessOrg(r.Context(), team.OrgID) {
			writeErrorResp(w, http.StatusNotFound, "key not found")
			return
		}
	}

	var in updateAPIKeyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if in.Name != "" {
		found.Name = in.Name
	}
	if in.TeamID != nil {
		found.TeamID = in.TeamID
	}
	if in.UserID != nil {
		found.UserID = in.UserID
	}
	if in.Models != nil {
		found.Models = in.Models
	}
	if in.RPMLimit != nil {
		found.RPMLimit = in.RPMLimit
	}
	if in.TPMLimit != nil {
		found.TPMLimit = in.TPMLimit
	}
	if in.MaxBudget != nil {
		found.MaxBudget = in.MaxBudget
	}
	if in.IsActive != nil {
		found.IsActive = *in.IsActive
	}

	if err := h.DB.UpdateAPIKey(r.Context(), found); err != nil {
		h.Logger.Error("update API key", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to update API key")
		return
	}
	writeJSON(w, http.StatusOK, found)
}
