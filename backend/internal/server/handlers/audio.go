package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"github.com/gateway-llm/gateway-llm/internal/types"
	"go.uber.org/zap"
)

func (h *Handlers) AudioSpeech(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()

	var req types.SpeechRequest
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
		prov, err := h.Registry.GetAudio(dep.Provider)
		if err != nil {
			return nil, err
		}
		reqCopy := req
		reqCopy.Model = dep.ProviderModel
		hreq, err := prov.TransformSpeechRequest(ctx, &reqCopy, dep.APIKey, dep.APIBase)
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

	prov, err := h.Registry.GetAudio(dep.Provider)
	if err != nil {
		resp.Body.Close()
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	rc, contentType, err := prov.StreamSpeechResponse(resp)
	if err != nil {
		writeErrorResp(w, http.StatusBadGateway, err.Error())
		return
	}
	defer rc.Close()

	uinfo := cost.UsageInfo{CharacterCount: len(req.Input)}
	cr, _ := h.CostEng.Calculate("audio/speech", dep.Provider, dep.ProviderModel, uinfo)
	setCostHeaders(w, cr)
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, rc); err != nil {
		h.Logger.Warn("audio speech copy", zap.Error(err))
	}
	h.logSpendAsync(start, key, dep, modelAlias, "audio/speech", http.StatusOK, uinfo, cr.TotalCost)
}

func (h *Handlers) AudioTranscriptions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	model := r.FormValue("model")
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
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for field, vals := range r.MultipartForm.Value {
			v := vals[0]
			if field == "model" {
				v = dep.ProviderModel
			}
			if err := mw.WriteField(field, v); err != nil {
				return nil, err
			}
		}
		for field, files := range r.MultipartForm.File {
			for _, fh := range files {
				wpart, err := mw.CreateFormFile(field, fh.Filename)
				if err != nil {
					return nil, err
				}
				f, err := fh.Open()
				if err != nil {
					return nil, err
				}
				if _, err := io.Copy(wpart, f); err != nil {
					f.Close()
					return nil, err
				}
				f.Close()
			}
		}
		if err := mw.Close(); err != nil {
			return nil, err
		}

		url := providerBaseURL(dep.APIBase) + "/v1/audio/transcriptions"
		hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
		if err != nil {
			return nil, err
		}
		hreq.Header.Set("Content-Type", mw.FormDataContentType())
		hreq.Header.Set("Authorization", "Bearer "+dep.APIKey)
		return http.DefaultClient.Do(hreq)
	})
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		writeErrorResp(w, resp.StatusCode, string(body))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeErrorResp(w, http.StatusBadGateway, err.Error())
		return
	}

	var out types.TranscriptionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		writeErrorResp(w, http.StatusBadGateway, "invalid upstream JSON")
		return
	}

	uinfo := cost.UsageInfo{DurationSeconds: out.Duration}
	cr, _ := h.CostEng.Calculate("audio/transcriptions", dep.Provider, dep.ProviderModel, uinfo)
	setCostHeaders(w, cr)
	writeJSON(w, http.StatusOK, out)
	h.logSpendAsync(start, key, dep, modelAlias, "audio/transcriptions", http.StatusOK, uinfo, cr.TotalCost)
}
