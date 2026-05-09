package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

func (h *Handlers) Moderations(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()

	var req types.ModerationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<22)).Decode(&req); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	model := req.Model
	if model == "" {
		writeErrorResp(w, http.StatusBadRequest, "model is required")
		return
	}

	key := middleware.GetAPIKey(ctx)
	if key == nil {
		writeErrorResp(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !middleware.CheckModelAccess(key, model) {
		writeErrorResp(w, http.StatusForbidden, "model not allowed for this API key")
		return
	}

	modelAlias := model

	resp, dep, err := h.Router.ExecuteWithFallbackForOrg(ctx, middleware.GetOrgID(ctx), modelAlias, func(ctx context.Context, dep *router.DeploymentInfo) (*http.Response, error) {
		prov, err := h.Registry.GetModeration(dep.Provider)
		if err != nil {
			return nil, err
		}
		reqCopy := req
		reqCopy.Model = dep.ProviderModel
		hreq, err := prov.TransformModerationRequest(ctx, &reqCopy, dep.APIKey, dep.APIBase)
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

	prov, err := h.Registry.GetModeration(dep.Provider)
	if err != nil {
		resp.Body.Close()
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := prov.TransformModerationResponse(resp)
	if err != nil {
		writeErrorResp(w, http.StatusBadGateway, err.Error())
		return
	}

	uinfo := cost.UsageInfo{PromptTokens: 0}
	cr, _ := h.CostEng.Calculate("moderations", dep.Provider, dep.ProviderModel, uinfo)
	setCostHeaders(w, cr)
	writeJSON(w, http.StatusOK, out)
	h.logSpendAsync(start, key, dep, modelAlias, "moderations", http.StatusOK, uinfo, cr.TotalCost)
}
