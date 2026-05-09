package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/savings"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
)

// ListSavings drives the /savings dashboard table. Org admins are
// scoped to their org; master / super admin can pass ?org_id= to
// filter or omit it for cross-tenant.
func (h *Handlers) ListSavings(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	q := r.URL.Query()
	f := db.SavingsFilter{Alias: q.Get("alias")}
	limit := 100
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	f.Limit = limit
	if v, err := strconv.Atoi(q.Get("offset")); err == nil && v >= 0 {
		f.Offset = v
	}
	if since := parseTimeParam(q.Get("since")); since != nil {
		f.Since = since
	}
	if until := parseTimeParam(q.Get("until")); until != nil {
		f.Until = until
	}
	if v := q.Get("api_key_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			f.APIKeyID = &id
		}
	}

	if middleware.IsMaster(r.Context()) || middleware.IsSuperAdmin(r.Context()) {
		if v := q.Get("org_id"); v != "" {
			if id, err := uuid.Parse(v); err == nil {
				f.OrgID = &id
			}
		}
	} else {
		f.OrgID = middleware.GetOrgID(r.Context())
		if f.OrgID == nil {
			writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
			return
		}
	}

	rows, err := h.DB.ListRoutingSavings(r.Context(), f)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []models.RoutingSavings{}
	}

	// Per-alias breakdown for the same window — small extra query but
	// keeps the dashboard one-roundtrip.
	since := time.Now().UTC().Add(-30 * 24 * time.Hour)
	until := time.Now().UTC()
	if f.Since != nil {
		since = *f.Since
	}
	if f.Until != nil {
		until = *f.Until
	}
	alias, err := h.DB.AggregateSavingsByAlias(r.Context(), f.OrgID, since, until)
	if err != nil && h.Logger != nil {
		alias = nil
	}
	if alias == nil {
		alias = []db.AliasSavingsRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":            rows,
		"alias_breakdown": alias,
	})
}

// ListDailySavings drives the time-series chart on /savings.
func (h *Handlers) ListDailySavings(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	q := r.URL.Query()
	since := time.Now().UTC().Add(-30 * 24 * time.Hour)
	until := time.Now().UTC()
	if t := parseTimeParam(q.Get("since")); t != nil {
		since = *t
	}
	if t := parseTimeParam(q.Get("until")); t != nil {
		until = *t
	}
	var orgID *uuid.UUID
	if middleware.IsMaster(r.Context()) || middleware.IsSuperAdmin(r.Context()) {
		if v := q.Get("org_id"); v != "" {
			if id, err := uuid.Parse(v); err == nil {
				orgID = &id
			}
		}
	} else {
		orgID = middleware.GetOrgID(r.Context())
	}
	rows, err := h.DB.ListDailySavings(r.Context(), orgID, since, until)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []models.DailySavings{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

// GetSavingsRecord returns one ledger row by id (drill-down view).
func (h *Handlers) GetSavingsRecord(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}
	row, err := h.DB.GetRoutingSavingsByID(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		writeErrorResp(w, http.StatusNotFound, "not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), row.OrgID) {
		writeErrorResp(w, http.StatusForbidden, "cannot read another org's ledger")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// GetSavingsByTrace lets the /usage drill-down jump straight to the
// ledger row using the trace id we already render in the usage table.
func (h *Handlers) GetSavingsByTrace(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	trace := chi.URLParam(r, "trace")
	if trace == "" {
		writeErrorResp(w, http.StatusBadRequest, "trace id required")
		return
	}
	row, err := h.DB.GetRoutingSavingsByTrace(r.Context(), trace)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		writeErrorResp(w, http.StatusNotFound, "no savings record for trace")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), row.OrgID) {
		writeErrorResp(w, http.StatusForbidden, "cannot read another org's ledger")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// VerifySavingsRecord runs the Ed25519 verify against the operator
// pubkey held on the receipt signer. Returns 200 with verified=true on
// success and 200 with verified=false + reason on failure (we don't
// 4xx because "verifying a row" is an idempotent read; a tampered row
// is still findable, just not trustable).
func (h *Handlers) VerifySavingsRecord(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	if h.Receipts == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "operator pubkey not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}
	row, err := h.DB.GetRoutingSavingsByID(r.Context(), id)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		writeErrorResp(w, http.StatusNotFound, "not found")
		return
	}
	if !middleware.CanAccessOrg(r.Context(), row.OrgID) {
		writeErrorResp(w, http.StatusForbidden, "cannot read another org's ledger")
		return
	}
	pub := h.Receipts.PublicKey()
	if err := savings.VerifyRow(pub, row); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"verified": false,
			"reason":   err.Error(),
			"row":      row,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"verified":         true,
		"signed_by_key_id": row.SignedByKeyID,
		"signed_at":        row.SignedAt,
		"row":              row,
	})
}

func parseTimeParam(v string) *time.Time {
	if v == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return &t
	}
	return nil
}
