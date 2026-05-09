package handlers

// LiteLLM compatibility surface.
//
// LiteLLM's proxy exposes a set of management endpoints that sit next to the
// OpenAI-compat surface. Existing LiteLLM deployments and tooling (the
// LiteLLM CLI, langchain-litellm, datadog-litellm integrations, etc.) call
// these paths by name. To let any LiteLLM user swap the base URL and keep
// their entire stack working, we expose the same endpoints here as thin
// translators onto our own management + spend + team APIs.
//
// This file is the single point of contact with LiteLLM-shaped JSON; every
// endpoint below accepts LiteLLM's request shape, maps to gateway-llm's
// internal handlers, and returns the response shape LiteLLM clients expect.

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// /model/info — list of available models with provider + cost + capabilities
// ---------------------------------------------------------------------------

type litellmModelInfo struct {
	ModelName string                 `json:"model_name"`
	ModelInfo map[string]interface{} `json:"model_info"`
	Params    map[string]interface{} `json:"litellm_params"`
}

func (h *Handlers) LiteLLMModelInfo(w http.ResponseWriter, r *http.Request) {
	aliases := h.Router.ListAliases()
	out := make([]litellmModelInfo, 0, len(aliases))
	for _, alias := range aliases {
		deps := h.Router.GetDeployments(alias)
		for _, d := range deps {
			info := map[string]interface{}{
				"id":       alias + "::" + d.Provider + "::" + d.ProviderModel,
				"provider": d.Provider,
				"mode":     "chat",
			}
			if h.CostEng != nil {
				if p := h.CostEng.GetPricing(d.Provider, d.ProviderModel); p != nil {
					info["input_cost_per_token"] = p.InputCostPerToken
					info["output_cost_per_token"] = p.OutputCostPerToken
					if p.MaxInputTokens > 0 {
						info["max_input_tokens"] = p.MaxInputTokens
					}
					if p.MaxOutputTokens > 0 {
						info["max_output_tokens"] = p.MaxOutputTokens
					}
				}
			}
			out = append(out, litellmModelInfo{
				ModelName: alias,
				ModelInfo: info,
				Params: map[string]interface{}{
					"model":    d.Provider + "/" + d.ProviderModel,
					"api_base": d.APIBase,
				},
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": out})
}

// ---------------------------------------------------------------------------
// /key/generate — LiteLLM's key minting endpoint
// Maps to our CreateAPIKey with a response shape LiteLLM SDKs recognize.
// ---------------------------------------------------------------------------

type litellmKeyRequest struct {
	KeyAlias   string    `json:"key_alias,omitempty"`
	Duration   string    `json:"duration,omitempty"`
	Models     []string  `json:"models,omitempty"`
	Aliases    map[string]string `json:"aliases,omitempty"`
	Config     map[string]any    `json:"config,omitempty"`
	Spend      *float64  `json:"spend,omitempty"`
	MaxBudget  *float64  `json:"max_budget,omitempty"`
	UserID     string    `json:"user_id,omitempty"`
	TeamID     string    `json:"team_id,omitempty"`
	MaxParallelRequests *int `json:"max_parallel_requests,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	TPMLimit   *int      `json:"tpm_limit,omitempty"`
	RPMLimit   *int      `json:"rpm_limit,omitempty"`
}

func (h *Handlers) LiteLLMKeyGenerate(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in litellmKeyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	raw, hash, err := generateGatewayLLMKey()
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to generate key")
		return
	}

	name := in.KeyAlias
	if name == "" {
		name = "litellm-ported-" + time.Now().Format("20060102-150405")
	}

	key := &models.APIKey{
		Name:      name,
		TokenHash: hash,
		Models:    in.Models,
		RPMLimit:  in.RPMLimit,
		TPMLimit:  in.TPMLimit,
		MaxBudget: in.MaxBudget,
		IsActive:  true,
	}
	if in.TeamID != "" {
		if tid, perr := uuid.Parse(in.TeamID); perr == nil {
			key.TeamID = &tid
		}
	}
	if in.UserID != "" {
		if uid, perr := uuid.Parse(in.UserID); perr == nil {
			key.UserID = &uid
		}
	}
	if in.Duration != "" {
		if d, perr := parseDuration(in.Duration); perr == nil {
			exp := time.Now().Add(d)
			key.ExpiresAt = &exp
		}
	}
	if err := h.DB.CreateAPIKey(r.Context(), key); err != nil {
		h.Logger.Warn("litellm key generate", zap.Error(err))
		writeErrorResp(w, http.StatusInternalServerError, "failed to create key")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"key":              raw,
		"key_alias":        name,
		"token":            raw,
		"expires":          key.ExpiresAt,
		"max_budget":       key.MaxBudget,
		"tpm_limit":        key.TPMLimit,
		"rpm_limit":        key.RPMLimit,
		"models":           key.Models,
		"user_id":          in.UserID,
		"team_id":          in.TeamID,
		"spend":            0,
	})
}

// parseDuration accepts LiteLLM-style durations like "30d", "24h", "15m".
// time.ParseDuration rejects the "d" suffix, so we pre-process it.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// ---------------------------------------------------------------------------
// /key/info — describe a key
// ---------------------------------------------------------------------------

func (h *Handlers) LiteLLMKeyInfo(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	keyStr := r.URL.Query().Get("key")
	if keyStr == "" {
		writeErrorResp(w, http.StatusBadRequest, "missing 'key' query parameter")
		return
	}
	// LiteLLM sometimes posts the hashed key, sometimes the raw. We try both.
	k, err := h.DB.GetAPIKeyByHash(r.Context(), hashToken(keyStr))
	if err != nil {
		k, err = h.DB.GetAPIKeyByHash(r.Context(), keyStr)
	}
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "key not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"key":        keyStr,
		"key_alias":  k.Name,
		"spend":      k.TotalSpend,
		"max_budget": k.MaxBudget,
		"tpm_limit":  k.TPMLimit,
		"rpm_limit":  k.RPMLimit,
		"expires":    k.ExpiresAt,
		"models":     k.Models,
		"user_id":    k.UserID,
		"team_id":    k.TeamID,
		"created_at": k.CreatedAt,
	})
}

// ---------------------------------------------------------------------------
// /key/delete
// ---------------------------------------------------------------------------

type litellmKeyDeleteRequest struct {
	Keys    []string `json:"keys,omitempty"`
	KeyAlias string  `json:"key_alias,omitempty"`
}

func (h *Handlers) LiteLLMKeyDelete(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in litellmKeyDeleteRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	deleted := 0
	for _, raw := range in.Keys {
		k, err := h.DB.GetAPIKeyByHash(r.Context(), hashToken(raw))
		if err != nil {
			continue
		}
		if err := h.DB.DeleteAPIKey(r.Context(), k.ID); err == nil {
			deleted++
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"deleted_keys": deleted})
}

// ---------------------------------------------------------------------------
// /spend/calculate — pricing preview
// ---------------------------------------------------------------------------

type litellmSpendCalculateRequest struct {
	Model            string `json:"model"`
	Messages         []any  `json:"messages,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionResponse any  `json:"completion_response,omitempty"`
}

func (h *Handlers) LiteLLMSpendCalculate(w http.ResponseWriter, r *http.Request) {
	var in litellmSpendCalculateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	alias := h.Router.ResolveCanonical(in.Model)
	deps := h.Router.GetDeployments(alias)
	if len(deps) == 0 {
		writeErrorResp(w, http.StatusNotFound, "model not found")
		return
	}
	d := deps[0]
	total := 0.0
	if h.CostEng != nil {
		cr, _ := h.CostEng.Calculate("chat", d.Provider, d.ProviderModel, cost.UsageInfo{
			PromptTokens:     in.PromptTokens,
			CompletionTokens: in.CompletionTokens,
		})
		if cr != nil {
			total = cr.TotalCost
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"model": in.Model,
		"cost":  total,
	})
}

// ---------------------------------------------------------------------------
// /spend/logs — paginated spend logs
// ---------------------------------------------------------------------------

func (h *Handlers) LiteLLMSpendLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	// Delegate to existing GetUsage with the same query shape.
	orig := r.URL.Query()
	orig.Set("limit", strconv.Itoa(limit))
	orig.Set("offset", strconv.Itoa(offset))
	r.URL.RawQuery = orig.Encode()
	h.GetUsage(w, r)
}

// ---------------------------------------------------------------------------
// /team/new, /team/info, /team/member_add
// ---------------------------------------------------------------------------

type litellmTeamNewRequest struct {
	TeamAlias string   `json:"team_alias,omitempty"`
	Admins    []string `json:"admins,omitempty"`
	Members   []string `json:"members,omitempty"`
	MaxBudget *float64 `json:"max_budget,omitempty"`
	Models    []string `json:"models,omitempty"`
	TPMLimit  *int     `json:"tpm_limit,omitempty"`
	RPMLimit  *int     `json:"rpm_limit,omitempty"`
	OrgID     string   `json:"organization_id,omitempty"`
}

func (h *Handlers) LiteLLMTeamNew(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in litellmTeamNewRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	t := &models.Team{
		Name:      in.TeamAlias,
		Models:    in.Models,
		RPMLimit:  in.RPMLimit,
		TPMLimit:  in.TPMLimit,
		MaxBudget: in.MaxBudget,
	}
	if in.OrgID != "" {
		if oid, perr := uuid.Parse(in.OrgID); perr == nil {
			t.OrgID = &oid
		}
	}
	if t.Name == "" {
		t.Name = "litellm-team-" + time.Now().Format("20060102-150405")
	}
	// Fall back to user's org if the caller is scoped.
	if t.OrgID == nil {
		if oid := middleware.GetOrgID(r.Context()); oid != nil {
			t.OrgID = oid
		}
	}
	if err := h.DB.CreateTeam(r.Context(), t); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"team_id":    t.ID.String(),
		"team_alias": t.Name,
		"max_budget": t.MaxBudget,
	})
}

func (h *Handlers) LiteLLMTeamInfo(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	tid := chi.URLParam(r, "team_id")
	if tid == "" {
		tid = r.URL.Query().Get("team_id")
	}
	if tid == "" {
		writeErrorResp(w, http.StatusBadRequest, "missing team_id")
		return
	}
	tuid, err := uuid.Parse(tid)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid team_id")
		return
	}
	t, err := h.DB.GetTeam(r.Context(), tuid)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "team not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"team_id":    t.ID.String(),
		"team_alias": t.Name,
		"max_budget": t.MaxBudget,
		"spend":      t.TotalSpend,
		"models":     t.Models,
		"rpm_limit":  t.RPMLimit,
		"tpm_limit":  t.TPMLimit,
	})
}

// ---------------------------------------------------------------------------
// /budget/new — minted as per-key budgets in our model.
// ---------------------------------------------------------------------------

type litellmBudgetRequest struct {
	BudgetID   string   `json:"budget_id,omitempty"`
	MaxBudget  *float64 `json:"max_budget,omitempty"`
	BudgetDuration string `json:"budget_duration,omitempty"`
	TPMLimit   *int     `json:"tpm_limit,omitempty"`
	RPMLimit   *int     `json:"rpm_limit,omitempty"`
}

func (h *Handlers) LiteLLMBudgetNew(w http.ResponseWriter, r *http.Request) {
	var in litellmBudgetRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// We don't currently store named budgets separately; echo the request so
	// LiteLLM callers get a success shape and can then apply the budget to a
	// key via /key/generate with max_budget set. Named budgets are a roadmap
	// item for the EE tier.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"budget_id":       in.BudgetID,
		"max_budget":      in.MaxBudget,
		"tpm_limit":       in.TPMLimit,
		"rpm_limit":       in.RPMLimit,
		"budget_duration": in.BudgetDuration,
	})
}

// ---------------------------------------------------------------------------
// /user/new, /user/info — thin wrappers over our CreateUser / GetUser.
// ---------------------------------------------------------------------------

type litellmUserNewRequest struct {
	UserEmail  string   `json:"user_email,omitempty"`
	UserRole   string   `json:"user_role,omitempty"`
	TeamID     string   `json:"team_id,omitempty"`
	MaxBudget  *float64 `json:"max_budget,omitempty"`
	Models     []string `json:"models,omitempty"`
}

func (h *Handlers) LiteLLMUserNew(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in litellmUserNewRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	u := &models.User{
		Email: in.UserEmail,
		Role:  mapLiteLLMRole(in.UserRole),
	}
	if in.TeamID != "" {
		if tid, perr := uuid.Parse(in.TeamID); perr == nil {
			u.TeamID = &tid
		}
	}
	if oid := middleware.GetOrgID(r.Context()); oid != nil {
		u.OrgID = oid
	}
	if err := h.DB.CreateUser(r.Context(), u); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":    u.ID.String(),
		"user_email": u.Email,
		"user_role":  u.Role,
		"team_id":    in.TeamID,
	})
}

// mapLiteLLMRole maps LiteLLM's "proxy_admin", "internal_user", etc. onto
// gateway-llm's role vocabulary. Unknown roles default to Member.
func mapLiteLLMRole(role string) string {
	switch strings.ToLower(role) {
	case "proxy_admin", "super_admin":
		return middleware.RoleSuperAdmin
	case "org_admin", "admin":
		return middleware.RoleOrgAdmin
	case "team_admin":
		return middleware.RoleTeamAdmin
	case "internal_user", "user", "member":
		return middleware.RoleMember
	case "viewer", "internal_user_viewer":
		return middleware.RoleViewer
	default:
		return middleware.RoleMember
	}
}

func (h *Handlers) LiteLLMUserInfo(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	uidStr := r.URL.Query().Get("user_id")
	if uidStr == "" {
		writeErrorResp(w, http.StatusBadRequest, "missing user_id")
		return
	}
	uid, err := uuid.Parse(uidStr)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	u, err := h.DB.GetUser(r.Context(), uid)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":    u.ID.String(),
		"user_email": u.Email,
		"user_role":  u.Role,
		"team_id":    u.TeamID,
	})
}

// ---------------------------------------------------------------------------
// /health/liveliness (LiteLLM spelling) and /health/readiness
// ---------------------------------------------------------------------------

func (h *Handlers) LiteLLMLiveliness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
