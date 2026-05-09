package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"github.com/gateway-llm/gateway-llm/internal/types"
	"go.uber.org/zap"
)

func (h *Handlers) CreateResponse(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()

	var req types.ResponsesRequest
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
	reqBody, _ := json.Marshal(req)

	resp, dep, err := h.Router.ExecuteWithFallbackForOrg(ctx, middleware.GetOrgID(ctx), modelAlias, func(ctx context.Context, dep *router.DeploymentInfo) (*http.Response, error) {
		prov, err := h.Registry.GetResponses(dep.Provider)
		if err != nil {
			return nil, err
		}
		reqCopy := req
		reqCopy.Model = dep.ProviderModel
		hreq, err := prov.TransformResponsesRequest(ctx, &reqCopy, dep.APIKey, dep.APIBase)
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
		prov, err := h.Registry.GetResponses(dep.Provider)
		if err != nil {
			resp.Body.Close()
			writeErrorResp(w, http.StatusInternalServerError, err.Error())
			return
		}
		ch, err := prov.StreamResponsesResponse(ctx, resp)
		if err != nil {
			resp.Body.Close()
			writeErrorResp(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flushWriter(w)

		var lastUsage *types.ResponsesUsage
		for evt := range ch {
			if evt.Error != nil {
				h.Logger.Warn("responses stream error", zap.Error(evt.Error))
				break
			}
			if evt.Done {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flushWriter(w)
				break
			}
			var envelope struct {
				Type     string                    `json:"type"`
				Response *types.ResponsesAPIResponse `json:"response,omitempty"`
			}
			if json.Unmarshal(evt.Data, &envelope) == nil && envelope.Response != nil && envelope.Response.Usage != nil {
				lastUsage = envelope.Response.Usage
			}
			fmt.Fprintf(w, "data: %s\n\n", string(evt.Data))
			flushWriter(w)
		}

		uinfo := usageFromResponses(lastUsage)
		cr, _ := h.CostEng.Calculate("responses", dep.Provider, dep.ProviderModel, uinfo)
		h.logSpendAsync(start, key, dep, modelAlias, "responses", http.StatusOK, uinfo, cr.TotalCost)
		return
	}

	prov, err := h.Registry.GetResponses(dep.Provider)
	if err != nil {
		resp.Body.Close()
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := prov.TransformResponsesResponse(resp)
	if err != nil {
		writeErrorResp(w, http.StatusBadGateway, err.Error())
		return
	}

	respBody, _ := json.Marshal(out)
	if h.DB != nil {
		var apiKeyID *uuid.UUID
		if key != nil && key.ID != uuid.Nil {
			apiKeyID = &key.ID
		}
		if err := h.DB.StoreResponseObject(ctx, out.ID, apiKeyID, dep.Provider, modelAlias, out.Status, reqBody, respBody); err != nil {
			h.Logger.Warn("store response object", zap.Error(err))
		}
	}

	uinfo := usageFromResponses(out.Usage)
	cr, _ := h.CostEng.Calculate("responses", dep.Provider, dep.ProviderModel, uinfo)
	setCostHeaders(w, cr)
	writeJSON(w, http.StatusOK, out)
	h.logSpendAsync(start, key, dep, modelAlias, "responses", http.StatusOK, uinfo, cr.TotalCost)
}

func (h *Handlers) GetResponse(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := chi.URLParam(r, "response_id")
	body, err := h.DB.GetResponseObject(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "response not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *Handlers) DeleteResponse(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id := chi.URLParam(r, "response_id")
	n, err := h.DB.DeleteResponseObject(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n == 0 {
		writeErrorResp(w, http.StatusNotFound, "response not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
