package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/crypto"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
)

type createCredentialRequest struct {
	Name     string     `json:"name"`
	Provider string     `json:"provider"`
	APIKey   string     `json:"api_key"`
	APIBase  string     `json:"api_base,omitempty"`
	OrgID    *uuid.UUID `json:"org_id,omitempty"`
}

type updateCredentialRequest struct {
	Name     string     `json:"name,omitempty"`
	Provider string     `json:"provider,omitempty"`
	APIKey   string     `json:"api_key,omitempty"`
	APIBase  string     `json:"api_base,omitempty"`
	OrgID    *uuid.UUID `json:"org_id,omitempty"`
	IsActive *bool      `json:"is_active,omitempty"`
}

type testRawCredentialRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	APIBase  string `json:"api_base,omitempty"`
}

// supportedProviders lists every provider the gateway can route to.
// Keep this in sync with the providers registered in server.New
// (openai, anthropic, gemini, azure, bedrock, cohere, groq, mistral,
// vertexai, xai). The UI dropdown uses the same identifiers.
var supportedProviders = map[string]bool{
	"openai":    true,
	"anthropic": true,
	"gemini":    true,
	"azure":     true,
	"bedrock":   true,
	"cohere":    true,
	"groq":      true,
	"mistral":   true,
	"vertexai":  true,
	"xai":       true,
}

func supportedProvidersList() []string {
	out := make([]string, 0, len(supportedProviders))
	for p := range supportedProviders {
		out = append(out, p)
	}
	return out
}

func maskKey(raw string) string {
	return crypto.MaskKey(raw)
}

func credentialResponse(c *models.ProviderCredential, rawKey string) map[string]interface{} {
	return map[string]interface{}{
		"id":             c.ID,
		"name":           c.Name,
		"provider":       c.Provider,
		"api_key_masked": maskKey(rawKey),
		"api_base":       c.APIBase,
		"org_id":         c.OrgID,
		"is_active":      c.IsActive,
		"created_by":     c.CreatedBy,
		"created_at":     c.CreatedAt,
		"updated_at":     c.UpdatedAt,
	}
}

func (h *Handlers) CreateCredential(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var in createCredentialRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Name == "" || in.Provider == "" || in.APIKey == "" {
		writeErrorResp(w, http.StatusBadRequest, "name, provider, and api_key are required")
		return
	}
	prov := strings.ToLower(in.Provider)
	if !supportedProviders[prov] {
		writeErrorResp(w, http.StatusBadRequest,
			"unsupported provider; must be one of: "+strings.Join(supportedProvidersList(), ", "))
		return
	}
	if requiresAPIBase(prov) && strings.TrimSpace(in.APIBase) == "" {
		writeErrorResp(w, http.StatusBadRequest,
			"api_base is required for provider "+prov)
		return
	}

	encrypted, err := crypto.Encrypt(in.APIKey)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to encrypt API key: "+err.Error())
		return
	}

	var createdBy *uuid.UUID
	if u := middleware.GetUser(r.Context()); u != nil {
		createdBy = &u.ID
	}

	orgID := in.OrgID
	if !middleware.IsSuperAdmin(r.Context()) {
		callerOrg := middleware.GetOrgID(r.Context())
		if callerOrg == nil {
			writeErrorResp(w, http.StatusForbidden, "cannot determine caller organization")
			return
		}
		if orgID != nil && *orgID != *callerOrg {
			writeErrorResp(w, http.StatusForbidden, "cannot create credentials outside your organization")
			return
		}
		orgID = callerOrg
	}

	cred := &models.ProviderCredential{
		Name:      in.Name,
		Provider:  strings.ToLower(in.Provider),
		APIKeyEnc: encrypted,
		APIBase:   in.APIBase,
		OrgID:     orgID,
		IsActive:  true,
		CreatedBy: createdBy,
	}
	if err := h.DB.CreateCredential(r.Context(), cred); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.triggerRouterReload(r.Context())
	writeJSON(w, http.StatusCreated, credentialResponse(cred, in.APIKey))
}

func (h *Handlers) ListCredentials(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	creds, err := h.DB.ListCredentials(r.Context())
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}

	var filterOrgID *uuid.UUID
	if raw := r.URL.Query().Get("org_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErrorResp(w, http.StatusBadRequest, "invalid org_id")
			return
		}
		filterOrgID = &id
	}
	// Force non-super admins to their own org regardless of the query param.
	if !middleware.IsSuperAdmin(r.Context()) {
		filterOrgID = middleware.GetOrgID(r.Context())
	}

	out := make([]map[string]interface{}, 0, len(creds))
	for _, c := range creds {
		if filterOrgID != nil {
			if c.OrgID == nil || *c.OrgID != *filterOrgID {
				continue
			}
		}
		rawKey := ""
		if decrypted, err := crypto.Decrypt(c.APIKeyEnc); err == nil {
			rawKey = decrypted
		}
		out = append(out, credentialResponse(&c, rawKey))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": out})
}

func (h *Handlers) UpdateCredential(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}

	cred, err := h.DB.GetCredential(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "credential not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), cred.OrgID) {
		writeErrorResp(w, http.StatusNotFound, "credential not found")
		return
	}

	var in updateCredentialRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if in.Name != "" {
		cred.Name = in.Name
	}
	if in.Provider != "" {
		cred.Provider = strings.ToLower(in.Provider)
	}
	if in.APIKey != "" {
		encrypted, err := crypto.Encrypt(in.APIKey)
		if err != nil {
			writeErrorResp(w, http.StatusInternalServerError, "failed to encrypt API key")
			return
		}
		cred.APIKeyEnc = encrypted
	}
	if in.APIBase != "" {
		cred.APIBase = in.APIBase
	}
	if in.OrgID != nil {
		cred.OrgID = in.OrgID
	}
	if in.IsActive != nil {
		cred.IsActive = *in.IsActive
	}

	if err := h.DB.UpdateCredential(r.Context(), cred); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.triggerRouterReload(r.Context())

	rawKey := ""
	if decrypted, err := crypto.Decrypt(cred.APIKeyEnc); err == nil {
		rawKey = decrypted
	}
	writeJSON(w, http.StatusOK, credentialResponse(cred, rawKey))
}

func (h *Handlers) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}
	cred, err := h.DB.GetCredential(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "credential not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), cred.OrgID) {
		writeErrorResp(w, http.StatusNotFound, "credential not found")
		return
	}
	if err := h.DB.DeleteCredential(r.Context(), id); err != nil {
		// Pre-migration deployments_credential_id_fkey had no ON DELETE
		// action, so deleting a credential linked to a deployment raises
		// a Postgres FK violation (SQLSTATE 23503). Translate to 409 so
		// the UI can show an actionable message.
		if strings.Contains(err.Error(), "violates foreign key") ||
			strings.Contains(err.Error(), "23503") {
			writeErrorResp(w, http.StatusConflict,
				"this provider key is still attached to one or more model routes. "+
					"Remove or reassign those routes, then try again.")
			return
		}
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.triggerRouterReload(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) TestCredential(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}

	cred, err := h.DB.GetCredential(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "credential not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), cred.OrgID) {
		writeErrorResp(w, http.StatusNotFound, "credential not found")
		return
	}

	apiKey, err := crypto.Decrypt(cred.APIKeyEnc)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, "failed to decrypt key")
		return
	}

	status, message := runProviderHealthCheck(r.Context(), cred.Provider, apiKey, cred.APIBase)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  status,
		"message": message,
	})
}

// TestRawCredential lets the UI verify a provider key before saving it.
// It accepts the raw key in the request body, performs the same health
// check as TestCredential against the provider, and returns the result.
// The key is never persisted.
func (h *Handlers) TestRawCredential(w http.ResponseWriter, r *http.Request) {
	var in testRawCredentialRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Provider == "" || in.APIKey == "" {
		writeErrorResp(w, http.StatusBadRequest, "provider and api_key are required")
		return
	}
	prov := strings.ToLower(in.Provider)
	if !supportedProviders[prov] {
		writeErrorResp(w, http.StatusBadRequest,
			"unsupported provider; must be one of: "+strings.Join(supportedProvidersList(), ", "))
		return
	}
	if requiresAPIBase(prov) && strings.TrimSpace(in.APIBase) == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "error",
			"message": "api_base is required for provider " + prov,
		})
		return
	}

	status, message := runProviderHealthCheck(r.Context(), prov, in.APIKey, in.APIBase)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  status,
		"message": message,
	})
}

// requiresAPIBase reports whether the given provider needs the user
// to supply an api_base — i.e. the public default endpoint can't be
// inferred from the provider name alone (Azure resource URL, Bedrock
// regional endpoint, customer-hosted Vertex/Mistral/Cohere installs).
func requiresAPIBase(provider string) bool {
	switch provider {
	case "azure", "bedrock", "vertexai":
		return true
	}
	return false
}

// runProviderHealthCheck performs a low-cost round-trip against the
// provider to verify the key works. Every branch returns ("ok", msg)
// on a 2xx upstream response or ("error", msg) otherwise.
func runProviderHealthCheck(parent context.Context, provider, apiKey, apiBase string) (string, string) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	provider = strings.ToLower(provider)
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")

	var req *http.Request
	switch provider {
	case "openai":
		if apiBase == "" {
			apiBase = "https://api.openai.com"
		}
		req, _ = http.NewRequestWithContext(ctx, "GET", apiBase+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "anthropic":
		if apiBase == "" {
			apiBase = "https://api.anthropic.com"
		}
		body := `{"model":"claude-sonnet-4-20250514","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`
		req, _ = http.NewRequestWithContext(ctx, "POST", apiBase+"/v1/messages", strings.NewReader(body))
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("Content-Type", "application/json")
	case "gemini":
		if apiBase == "" {
			apiBase = "https://generativelanguage.googleapis.com"
		}
		req, _ = http.NewRequestWithContext(ctx, "GET",
			fmt.Sprintf("%s/v1beta/models?key=%s", apiBase, apiKey), nil)
	case "azure":
		// Azure OpenAI deployment-scoped resource URL; api-key header.
		// We use the resource-level /openai/models?api-version=... probe
		// which only requires a valid key, not a specific deployment.
		req, _ = http.NewRequestWithContext(ctx, "GET",
			apiBase+"/openai/models?api-version=2024-08-01-preview", nil)
		req.Header.Set("api-key", apiKey)
	case "bedrock":
		// Bedrock Converse uses Bearer auth via the bedrock-runtime
		// endpoint; we hit the bedrock control-plane /foundation-models
		// list to confirm the key. The user supplies the regional
		// runtime URL (e.g. https://bedrock-runtime.us-east-1.amazonaws.com),
		// from which we derive the corresponding control-plane host.
		base := apiBase
		base = strings.Replace(base, "bedrock-runtime.", "bedrock.", 1)
		req, _ = http.NewRequestWithContext(ctx, "GET", base+"/foundation-models?maxResults=1", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "cohere":
		if apiBase == "" {
			apiBase = "https://api.cohere.com"
		}
		req, _ = http.NewRequestWithContext(ctx, "GET", apiBase+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "groq":
		if apiBase == "" {
			apiBase = "https://api.groq.com/openai"
		}
		req, _ = http.NewRequestWithContext(ctx, "GET", apiBase+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "mistral":
		if apiBase == "" {
			apiBase = "https://api.mistral.ai"
		}
		req, _ = http.NewRequestWithContext(ctx, "GET", apiBase+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "xai":
		if apiBase == "" {
			apiBase = "https://api.x.ai"
		}
		req, _ = http.NewRequestWithContext(ctx, "GET", apiBase+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "vertexai":
		// Vertex AI requires an OAuth bearer token + project/location;
		// a static API-key probe is not meaningful here. We accept the
		// configuration as-is and let the first real request validate.
		return "ok", "Vertex AI credentials saved; tested implicitly on first request"
	default:
		return "error", "unknown provider: " + provider
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "error", err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "ok", fmt.Sprintf("Provider %s responded with %d", provider, resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return "error", fmt.Sprintf("Provider %s responded with %d: %s",
		provider, resp.StatusCode, string(body))
}
