package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/models"
)

func (h *Handlers) ListPricing(w http.ResponseWriter, r *http.Request) {
	all := h.CostEng.GetAllPricing()
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	list := make([]*cost.ModelPricing, 0, len(keys))
	for _, k := range keys {
		list = append(list, all[k])
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": list})
}

func (h *Handlers) GetModelPricing(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	model := chi.URLParam(r, "model")
	if provider == "" || model == "" {
		writeErrorResp(w, http.StatusBadRequest, "provider and model are required")
		return
	}
	p := h.CostEng.GetPricing(provider, model)
	if p == nil {
		writeErrorResp(w, http.StatusNotFound, "pricing not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handlers) SetModelPricing(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	provider := chi.URLParam(r, "provider")
	model := chi.URLParam(r, "model")
	if provider == "" || model == "" {
		writeErrorResp(w, http.StatusBadRequest, "provider and model are required")
		return
	}
	var body models.CustomPricing
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body.Provider = provider
	body.Model = model
	body.DeploymentID = nil
	if err := h.DB.UpsertCustomPricing(r.Context(), &body); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.CostEng.LoadCustomPricing(r.Context()); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) DeleteModelPricing(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	provider := chi.URLParam(r, "provider")
	model := chi.URLParam(r, "model")
	if provider == "" || model == "" {
		writeErrorResp(w, http.StatusBadRequest, "provider and model are required")
		return
	}
	if err := h.DB.DeleteCustomPricing(r.Context(), provider, model); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.CostEng.LoadCustomPricing(r.Context()); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
