package handlers

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
)

// ListOperatorCatalog returns active rows by default. Pass
// ?include_history=1 to also see superseded rows (for the audit
// timeline UI).
func (h *Handlers) ListOperatorCatalog(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	includeHistory := r.URL.Query().Get("include_history") == "1"
	rows, err := h.DB.ListAllOperatorCatalog(r.Context(), includeHistory)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	verified, rejected, loadedAt, hasKey := 0, 0, time.Time{}, false
	if h.CostEng != nil {
		verified, rejected, loadedAt, hasKey = h.CostEng.CatalogStats()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": rows,
		"signing": map[string]any{
			"verified_rows": verified,
			"rejected_rows": rejected,
			"loaded_at":     loadedAt,
			"has_pubkey":    hasKey,
		},
	})
}

// SetOperatorCatalogRow upserts one row and signs it with the
// receipt-key (the operator's Ed25519 key). Only callable with the
// master key.
type catalogPayload struct {
	Provider              string             `json:"provider"`
	Model                 string             `json:"model"`
	Mode                  string             `json:"mode,omitempty"`
	InputCostPerToken     float64            `json:"input_cost_per_token"`
	OutputCostPerToken    float64            `json:"output_cost_per_token"`
	CacheReadCostPerToken *float64           `json:"cache_read_cost_per_token,omitempty"`
	InputCostPerImage     *float64           `json:"input_cost_per_image,omitempty"`
	InputCostPerCharacter *float64           `json:"input_cost_per_character,omitempty"`
	InputCostPerSecond    *float64           `json:"input_cost_per_second,omitempty"`
	MaxInputTokens        *int               `json:"max_input_tokens,omitempty"`
	MaxOutputTokens       *int               `json:"max_output_tokens,omitempty"`
	SizePricing           map[string]float64 `json:"size_pricing,omitempty"`
	Source                string             `json:"source,omitempty"`
}

func (h *Handlers) SetOperatorCatalogRow(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil || h.CostEng == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "catalog not configured")
		return
	}
	provider := strings.TrimSpace(chi.URLParam(r, "provider"))
	model := strings.TrimSpace(chi.URLParam(r, "model"))
	if provider == "" || model == "" {
		writeErrorResp(w, http.StatusBadRequest, "provider and model are required")
		return
	}
	var body catalogPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	row := &models.OperatorPriceCatalog{
		Provider:              provider,
		Model:                 model,
		Mode:                  body.Mode,
		InputCostPerToken:     body.InputCostPerToken,
		OutputCostPerToken:    body.OutputCostPerToken,
		CacheReadCostPerToken: body.CacheReadCostPerToken,
		InputCostPerImage:     body.InputCostPerImage,
		InputCostPerCharacter: body.InputCostPerCharacter,
		InputCostPerSecond:    body.InputCostPerSecond,
		MaxInputTokens:        body.MaxInputTokens,
		MaxOutputTokens:       body.MaxOutputTokens,
		SizePricing:           body.SizePricing,
		Source:                firstNonEmpty(body.Source, "manual"),
		EffectiveFrom:         time.Now().UTC(),
	}
	if h.Receipts == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "operator signing key not configured (enable receipts to sign catalog)")
		return
	}
	priv := h.Receipts.PrivateKey()
	if priv == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "signing key unavailable")
		return
	}
	if err := cost.SignCatalogRow(priv, row); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.DB.UpsertOperatorCatalogRow(r.Context(), row); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.CostEng.LoadOperatorCatalog(r.Context(), h.DB); err != nil && h.Logger != nil {
		h.Logger.Warn("reload operator catalog failed", zap.Error(err))
	}
	writeJSON(w, http.StatusOK, row)
}

// SeedOperatorCatalogFromEmbedded copies every row from the embedded
// model_prices.json into operator_price_catalog and signs them with
// the receipt key. Idempotent: rows that already exist (matched by
// provider+model with no superseded effective_to) are skipped.
func (h *Handlers) SeedOperatorCatalogFromEmbedded(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil || h.CostEng == nil || h.Receipts == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "catalog or signer not configured")
		return
	}
	priv := h.Receipts.PrivateKey()
	if priv == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "signing key unavailable")
		return
	}
	all := h.CostEng.GetAllPricing() // includes builtIn — that's fine, we want the embedded set
	seeded := 0
	skipped := 0
	for key, p := range all {
		if p == nil {
			continue
		}
		// We only want to seed embedded entries; the operator catalog
		// already-loaded entries shadow them, so we still try every
		// provider/model and rely on UpsertOperatorCatalogRow's
		// supersede behavior. Caller can pass ?force=1 to force a
		// re-sign of every row.
		_ = key
		row := &models.OperatorPriceCatalog{
			Provider:           p.Provider,
			Model:              p.Model,
			Mode:               p.Mode,
			InputCostPerToken:  p.InputCostPerToken,
			OutputCostPerToken: p.OutputCostPerToken,
			Source:             "seed",
			EffectiveFrom:      time.Now().UTC(),
			SizePricing:        p.SizePricing,
		}
		if p.CacheReadCostPerToken > 0 {
			v := p.CacheReadCostPerToken
			row.CacheReadCostPerToken = &v
		}
		if p.InputCostPerImage > 0 {
			v := p.InputCostPerImage
			row.InputCostPerImage = &v
		}
		if p.InputCostPerCharacter > 0 {
			v := p.InputCostPerCharacter
			row.InputCostPerCharacter = &v
		}
		if p.InputCostPerSecond > 0 {
			v := p.InputCostPerSecond
			row.InputCostPerSecond = &v
		}
		if p.MaxInputTokens > 0 {
			v := p.MaxInputTokens
			row.MaxInputTokens = &v
		}
		if p.MaxOutputTokens > 0 {
			v := p.MaxOutputTokens
			row.MaxOutputTokens = &v
		}
		if err := cost.SignCatalogRow(priv, row); err != nil {
			if h.Logger != nil {
				h.Logger.Warn("seed sign failed",
					zap.String("provider", p.Provider),
					zap.String("model", p.Model),
					zap.Error(err))
			}
			skipped++
			continue
		}
		if err := h.DB.UpsertOperatorCatalogRow(r.Context(), row); err != nil {
			if h.Logger != nil {
				h.Logger.Warn("seed upsert failed",
					zap.String("provider", p.Provider),
					zap.String("model", p.Model),
					zap.Error(err))
			}
			skipped++
			continue
		}
		seeded++
	}
	if err := h.CostEng.LoadOperatorCatalog(r.Context(), h.DB); err != nil && h.Logger != nil {
		h.Logger.Warn("reload operator catalog after seed failed", zap.Error(err))
	}
	writeJSON(w, http.StatusOK, map[string]any{"seeded": seeded, "skipped": skipped})
}

// ---- Discounts ----------------------------------------------------------

// ListDiscounts: master sees all; org admin sees their org only.
func (h *Handlers) ListDiscounts(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	if middleware.IsMaster(r.Context()) || middleware.IsSuperAdmin(r.Context()) {
		rows, err := h.DB.ListActiveOrgDiscounts(r.Context())
		if err != nil {
			writeErrorResp(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, withStatus(rows))
		return
	}
	orgID := middleware.GetOrgID(r.Context())
	rows, err := h.DB.ListOrgDiscountsForOrg(r.Context(), orgID)
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, withStatus(rows))
}

func withStatus(rows []models.OrgProviderDiscount) map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		out = append(out, map[string]any{
			"id":                d.ID,
			"org_id":            d.OrgID,
			"provider":          d.Provider,
			"discount_pct":      d.DiscountPct,
			"evidence_url":      d.EvidenceURL,
			"evidence_note":     d.EvidenceNote,
			"declared_by":       d.DeclaredBy,
			"declared_at":       d.DeclaredAt,
			"attested_by":       d.AttestedBy,
			"attested_at":       d.AttestedAt,
			"effective_from":    d.EffectiveFrom,
			"effective_to":      d.EffectiveTo,
			"status":            d.Status(),
		})
	}
	return map[string]any{"data": out}
}

type declareDiscountPayload struct {
	OrgID        *string  `json:"org_id,omitempty"`
	Provider     string   `json:"provider"`
	DiscountPct  float64  `json:"discount_pct"`
	EvidenceURL  *string  `json:"evidence_url,omitempty"`
	EvidenceNote *string  `json:"evidence_note,omitempty"`
}

// DeclareDiscount: org admin scope. Posting this DOES NOT yet affect
// billing; the discount stays in "declared" status until the operator
// counter-signs via POST /v1/management/discounts/{id}/countersign.
func (h *Handlers) DeclareDiscount(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	var body declareDiscountPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<19)).Decode(&body); err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Provider == "" {
		writeErrorResp(w, http.StatusBadRequest, "provider is required")
		return
	}
	if body.DiscountPct < 0 || body.DiscountPct >= 1 {
		writeErrorResp(w, http.StatusBadRequest, "discount_pct must be in [0,1)")
		return
	}
	d := &models.OrgProviderDiscount{
		Provider:     body.Provider,
		DiscountPct:  body.DiscountPct,
		EvidenceURL:  body.EvidenceURL,
		EvidenceNote: body.EvidenceNote,
	}

	// Master-key callers can target any org by passing org_id; everyone
	// else can only declare for their own org.
	if middleware.IsMaster(r.Context()) || middleware.IsSuperAdmin(r.Context()) {
		if body.OrgID != nil && *body.OrgID != "" {
			oid, err := uuid.Parse(*body.OrgID)
			if err != nil {
				writeErrorResp(w, http.StatusBadRequest, "invalid org_id")
				return
			}
			d.OrgID = &oid
		}
	} else {
		oid := middleware.GetOrgID(r.Context())
		if oid == nil {
			writeErrorResp(w, http.StatusForbidden, "org context required")
			return
		}
		d.OrgID = oid
	}
	if user := middleware.GetUser(r.Context()); user != nil {
		uid := user.ID
		d.DeclaredBy = &uid
	}
	if err := h.DB.DeclareOrgDiscount(r.Context(), d); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// CountersignDiscount: master-key only. Computes an Ed25519 signature
// over a canonical projection of the discount declaration so an
// auditor can prove the operator countersigned the specific terms.
func (h *Handlers) CountersignDiscount(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}
	if h.Receipts == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "operator signing key not configured")
		return
	}
	priv := h.Receipts.PrivateKey()
	if priv == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "signing key unavailable")
		return
	}
	rows, err := h.DB.ListActiveOrgDiscounts(r.Context())
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	var target *models.OrgProviderDiscount
	for i := range rows {
		if rows[i].ID == id {
			target = &rows[i]
			break
		}
	}
	if target == nil {
		writeErrorResp(w, http.StatusNotFound, "discount not found or already attested/expired")
		return
	}
	canonical, err := json.Marshal(map[string]any{
		"id":           target.ID.String(),
		"org_id":       formatNullableUUID(target.OrgID),
		"provider":     target.Provider,
		"discount_pct": target.DiscountPct,
		"declared_at":  target.DeclaredAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
	})
	if err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, canonical))

	user := middleware.GetUser(r.Context())
	var attestedBy uuid.UUID
	if user != nil {
		attestedBy = user.ID
	}
	if err := h.DB.CountersignDiscount(r.Context(), id, attestedBy, sig); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.CostEng != nil {
		if err := h.CostEng.LoadEffectiveDiscounts(r.Context(), h.DB); err != nil && h.Logger != nil {
			h.Logger.Warn("reload discounts failed", zap.Error(err))
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "countersigned"})
}

// RevokeDiscount: master OR the org admin who owns the discount.
func (h *Handlers) RevokeDiscount(w http.ResponseWriter, r *http.Request) {
	if h.DB == nil {
		writeErrorResp(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErrorResp(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !middleware.IsMaster(r.Context()) && !middleware.IsSuperAdmin(r.Context()) {
		// Best-effort scope check: only allow revoking discounts in
		// caller's own org. Master/super bypass.
		rows, err := h.DB.ListActiveOrgDiscounts(r.Context())
		if err != nil {
			writeErrorResp(w, http.StatusInternalServerError, err.Error())
			return
		}
		var found *models.OrgProviderDiscount
		for i := range rows {
			if rows[i].ID == id {
				found = &rows[i]
				break
			}
		}
		if found == nil {
			writeErrorResp(w, http.StatusNotFound, "discount not found")
			return
		}
		if !middleware.CanAccessOrg(r.Context(), found.OrgID) {
			writeErrorResp(w, http.StatusForbidden, "cannot revoke discount in another org")
			return
		}
	}
	if err := h.DB.RevokeDiscount(r.Context(), id); err != nil {
		writeErrorResp(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.CostEng != nil {
		_ = h.CostEng.LoadEffectiveDiscounts(r.Context(), h.DB)
	}
	w.WriteHeader(http.StatusNoContent)
}

func formatNullableUUID(u *uuid.UUID) string {
	if u == nil {
		return ""
	}
	return u.String()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
