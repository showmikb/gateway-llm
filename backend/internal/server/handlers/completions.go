package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"github.com/gateway-llm/gateway-llm/internal/types"
	"go.uber.org/zap"
)

func (h *Handlers) Completions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()

	var req types.CompletionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<22)).Decode(&req); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	key := middleware.GetAPIKey(ctx)
	if key == nil {
		writeErrorResp(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !middleware.CheckModelAccess(key, req.Model) {
		writeErrorResp(w, http.StatusForbidden, "model not allowed for this API key")
		return
	}

	modelAlias := req.Model

	resp, dep, err := h.Router.ExecuteWithFallbackForOrg(ctx, middleware.GetOrgID(ctx), modelAlias, func(ctx context.Context, dep *router.DeploymentInfo) (*http.Response, error) {
		prov, err := h.Registry.GetCompletions(dep.Provider)
		if err != nil {
			return nil, err
		}
		reqCopy := req
		reqCopy.Model = dep.ProviderModel
		hreq, err := prov.TransformCompletionsRequest(ctx, &reqCopy, dep.APIKey, dep.APIBase)
		if err != nil {
			return nil, err
		}
		return http.DefaultClient.Do(hreq)
	})
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, err.Error())
		return
	}

	if forwardUpstreamJSONError(w, resp) {
		return
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flushWriter(w)
		_, copyErr := io.Copy(w, resp.Body)
		resp.Body.Close()
		if copyErr != nil {
			h.Logger.Error("completions stream copy", zap.Error(copyErr))
		}
		uinfo := usageFromChat(nil)
		cr, _ := h.CostEng.Calculate("completions", dep.Provider, dep.ProviderModel, uinfo)
		h.logSpendAsync(start, key, dep, modelAlias, "completions", http.StatusOK, uinfo, cr.TotalCost)
		return
	}

	prov, err := h.Registry.GetCompletions(dep.Provider)
	if err != nil {
		resp.Body.Close()
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := prov.TransformCompletionsResponse(resp)
	if err != nil {
		writeErrorResp(w, http.StatusBadGateway, err.Error())
		return
	}

	uinfo := usageFromChat(out.Usage)
	cr, _ := h.CostEng.Calculate("completions", dep.Provider, dep.ProviderModel, uinfo)
	setCostHeaders(w, cr)
	writeJSON(w, http.StatusOK, out)
	h.logSpendAsync(start, key, dep, modelAlias, "completions", http.StatusOK, uinfo, cr.TotalCost)
}
