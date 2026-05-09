package handlers

// Replay + online-eval HTTP surface.
//
// The endpoints under /v1/replay and /v1/eval are the UI's window into
// pillar 1 of the product (Record & Replay + Online Eval). They are thin
// controllers that translate HTTP into replay.Engine calls and back.

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/replay"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
)

type startReplayRequest struct {
	Name        string          `json:"name"`
	TargetAlias string          `json:"target_alias"`
	Scorer      string          `json:"scorer"`
	ScorerCfg   json.RawMessage `json:"scorer_config,omitempty"`
	Filter      replayFilter    `json:"filter,omitempty"`
}

type replayFilter struct {
	ModelAlias string `json:"model_alias,omitempty"`
	Tag        string `json:"tag,omitempty"`
	APIKeyID   string `json:"api_key_id,omitempty"`
	TraceID    string `json:"trace_id,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

// StartReplayRun creates a new replay run.
// POST /v1/replay/runs
func (h *Handlers) StartReplayRun(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil || h.ReplayEngine == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "replay not configured")
		return
	}
	var in startReplayRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	filter := db.ListRecordingsFilter{
		ModelAlias: in.Filter.ModelAlias,
		Tag:        in.Filter.Tag,
		TraceID:    in.Filter.TraceID,
		Limit:      in.Filter.Limit,
	}
	if in.Filter.APIKeyID != "" {
		if id, err := uuid.Parse(in.Filter.APIKeyID); err == nil {
			filter.APIKeyID = &id
		}
	}
	if oid := middleware.GetOrgID(r.Context()); oid != nil {
		filter.OrgID = oid
	}
	var createdBy *uuid.UUID
	if k := middleware.GetAPIKey(r.Context()); k != nil && k.UserID != nil {
		createdBy = k.UserID
	}
	run, err := h.ReplayEngine.Start(r.Context(), replay.RunOptions{
		Name:        in.Name,
		OrgID:       filter.OrgID,
		CreatedBy:   createdBy,
		TargetAlias: in.TargetAlias,
		Scorer:      in.Scorer,
		ScorerCfg:   in.ScorerCfg,
		Filter:      filter,
	})
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

// GetReplayRun returns a single replay run's progress/status.
// GET /v1/replay/runs/{id}
func (h *Handlers) GetReplayRun(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid run id")
		return
	}
	run, err := h.DB.GetReplayRun(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// ListReplayResults paginates results for a run.
// GET /v1/replay/runs/{id}/results?limit=&offset=
func (h *Handlers) ListReplayResults(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid run id")
		return
	}
	limit := 100
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}
	results, err := h.DB.ListReplayResults(r.Context(), id, limit, offset)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": results})
}

// ListRecordings paginates recordings.
// GET /v1/recordings
func (h *Handlers) ListRecordings(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	filter := db.ListRecordingsFilter{
		ModelAlias: r.URL.Query().Get("model"),
		Tag:        r.URL.Query().Get("tag"),
		TraceID:    r.URL.Query().Get("trace_id"),
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Offset = n
		}
	}
	if oid := middleware.GetOrgID(r.Context()); oid != nil {
		filter.OrgID = oid
	}
	recs, err := h.DB.ListRecordings(r.Context(), filter)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": recs})
}

// GetRecording returns a single recording with its blob payloads inlined.
// GET /v1/recordings/{id}?inline=true
func (h *Handlers) GetRecording(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid recording id")
		return
	}
	rec, err := h.DB.GetRecording(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusNotFound, "recording not found")
		return
	}
	out := map[string]interface{}{"recording": rec}
	if r.URL.Query().Get("inline") == "true" && h.BlobStore != nil {
		if rec.RequestBlob != "" {
			if b, err := h.BlobStore.Get(r.Context(), rec.RequestBlob); err == nil {
				out["request"] = json.RawMessage(b)
			}
		}
		if rec.ResponseBlob != "" {
			if b, err := h.BlobStore.Get(r.Context(), rec.ResponseBlob); err == nil {
				out["response"] = json.RawMessage(b)
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ScoreRecording runs an online-eval scorer against a recording and stores
// the score. Useful for computing a new metric over historical data.
// POST /v1/eval/recordings/{id}
// body: { "scorer": "cosine", "scorer_config": { ... } }
func (h *Handlers) ScoreRecording(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil || h.BlobStore == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "eval not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid recording id")
		return
	}
	var in struct {
		Scorer       string          `json:"scorer"`
		ScorerConfig json.RawMessage `json:"scorer_config,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	scorer, err := replay.NewScorerByName(in.Scorer, in.ScorerConfig)
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, err.Error())
		return
	}
	score, err := replay.EvalOne(r.Context(), h.DB, h.BlobStore, id, scorer)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, score)
}

// ListEvalScores returns all scores attached to a recording.
// GET /v1/eval/recordings/{id}
func (h *Handlers) ListEvalScores(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid recording id")
		return
	}
	scores, err := h.DB.ListEvalScores(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": scores})
}
