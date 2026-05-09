package handlers

import (
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gateway-llm/gateway-llm/internal/types"
)

func (h *Handlers) ListModels(w http.ResponseWriter, r *http.Request) {
	aliases := h.Router.ListAliases()
	sort.Strings(aliases)
	now := time.Now().Unix()
	data := make([]types.Model, 0, len(aliases))
	for _, id := range aliases {
		data = append(data, types.Model{
			ID:      id,
			Object:  "model",
			Created: now,
			OwnedBy: "gateway-llm",
		})
	}
	writeJSON(w, http.StatusOK, types.ModelList{Object: "list", Data: data})
}

func (h *Handlers) GetModel(w http.ResponseWriter, r *http.Request) {
	modelID := chi.URLParam(r, "model_id")
	for _, a := range h.Router.ListAliases() {
		if a == modelID {
			writeJSON(w, http.StatusOK, types.Model{
				ID:      modelID,
				Object:  "model",
				Created: time.Now().Unix(),
				OwnedBy: "gateway-llm",
			})
			return
		}
	}
	writeErrorResp(w, http.StatusNotFound, "model not found")
}
