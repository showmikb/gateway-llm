package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/config"
	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"go.uber.org/zap"
)

// OpenAIPassthrough forwards any /v1/* request that does not match a
// typed Gateway-LLM handler to an upstream OpenAI-compatible provider.
// Auth, rate-limiting, and audit middleware are already applied by the
// /v1 route group before this handler runs.
func (h *Handlers) OpenAIPassthrough(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()
	cfg := h.Cfg.OpenAICompat

	if !cfg.Enabled {
		writeErrorResp(w, http.StatusNotFound, "endpoint not found")
		return
	}

	path := r.URL.Path
	if !isAllowedPassthrough(path, cfg) {
		writeErrorResp(w, http.StatusNotFound, "endpoint not found")
		return
	}

	key := middleware.GetAPIKey(ctx)
	if key == nil {
		writeErrorResp(w, http.StatusUnauthorized, "authentication required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	modelAlias := extractModelFromBody(body, r)
	if modelAlias == "" {
		modelAlias = cfg.DefaultModelAlias
	}
	if modelAlias == "" {
		writeErrorResp(w, http.StatusBadRequest,
			"could not determine model alias; set openai_compat.default_model_alias in gateway config or include a \"model\" field in the request")
		return
	}

	if !middleware.CheckModelAccess(key, modelAlias) {
		writeErrorResp(w, http.StatusForbidden, "model not allowed for this API key")
		return
	}

	resolvedAlias := h.Router.ResolveCanonical(modelAlias)

	rewrittenBody := rewriteModelInBody(body, r, resolvedAlias)

	resp, dep, err := h.Router.ExecuteWithFallbackForOrg(ctx, middleware.GetOrgID(ctx), resolvedAlias,
		func(ctx context.Context, dep *router.DeploymentInfo) (*http.Response, error) {
			prov, err := h.Registry.GetPassthrough(dep.Provider)
			if err != nil {
				return nil, err
			}

			finalBody := rewriteModelInBody(body, r, dep.ProviderModel)

			hreq, err := prov.ForwardRequest(ctx, r.Method, path, r.URL.RawQuery, r.Header, bytes.NewReader(finalBody), dep.APIKey, dep.APIBase)
			if err != nil {
				return nil, err
			}
			return http.DefaultClient.Do(hreq)
		})
	_ = rewrittenBody

	if err != nil {
		h.Logger.Warn("passthrough upstream failed", zap.String("path", path), zap.Error(err))
		writeErrorResp(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		switch strings.ToLower(k) {
		case "transfer-encoding", "connection":
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	w.WriteHeader(resp.StatusCode)
	if isSSE {
		flushWriter(w)
	}

	if isSSE {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, wErr := w.Write(buf[:n]); wErr != nil {
					break
				}
				flushWriter(w)
			}
			if readErr != nil {
				break
			}
		}
	} else {
		_, _ = io.Copy(w, resp.Body)
	}

	uinfo := cost.UsageInfo{}
	if dep != nil {
		cr, _ := h.CostEng.Calculate("passthrough", dep.Provider, dep.ProviderModel, uinfo)
		h.logSpendAsync(start, key, dep, resolvedAlias, "passthrough:"+path, resp.StatusCode, uinfo, cr.TotalCost)
	}
}

func isAllowedPassthrough(path string, cfg config.OpenAICompatConfig) bool {
	for _, prefix := range cfg.BlockedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	if len(cfg.AllowedPrefixes) == 0 {
		return true
	}
	for _, prefix := range cfg.AllowedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// extractModelFromBody tries to find a "model" field in JSON or
// multipart form data without fully parsing the body.
func extractModelFromBody(body []byte, r *http.Request) string {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") && len(body) > 0 {
		var peek struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(body, &peek) == nil && peek.Model != "" {
			return peek.Model
		}
	}
	if strings.HasPrefix(ct, "multipart/form-data") {
		if r.MultipartForm != nil {
			if vals := r.MultipartForm.Value["model"]; len(vals) > 0 && vals[0] != "" {
				return vals[0]
			}
		}
	}
	return ""
}

// rewriteModelInBody replaces the "model" field in a JSON body with
// the target model name. For non-JSON or bodies without a model field,
// returns the original bytes unchanged.
func rewriteModelInBody(body []byte, r *http.Request, targetModel string) []byte {
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") || len(body) == 0 || targetModel == "" {
		return body
	}
	var parsed map[string]json.RawMessage
	if json.Unmarshal(body, &parsed) != nil {
		return body
	}
	if _, ok := parsed["model"]; !ok {
		return body
	}
	modelJSON, err := json.Marshal(targetModel)
	if err != nil {
		return body
	}
	parsed["model"] = modelJSON
	rewritten, err := json.Marshal(parsed)
	if err != nil {
		return body
	}
	return rewritten
}
