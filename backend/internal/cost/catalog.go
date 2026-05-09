package cost

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/models"
	"go.uber.org/zap"
)

// CatalogCanonical is the deterministic projection of an operator
// catalog row used for signing and verification. It MUST stay in sync
// with the columns the DB writer reads back. We intentionally exclude
// id, created_at, signed_at, signature, and signed_by_key_id from the
// signed bytes — those are envelope metadata, not the priced facts.
type CatalogCanonical struct {
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
	EffectiveFrom         string             `json:"effective_from"` // RFC3339Nano UTC
}

// CanonicalBytes returns the bytes that are signed for a catalog row.
// Matches CatalogCanonical's field order exactly. effective_from is
// rendered as RFC3339Nano UTC so the signed string survives a Postgres
// round-trip with microsecond truncation as long as we sign the
// truncated value (the writer normalizes before signing).
func CanonicalCatalogBytes(c *models.OperatorPriceCatalog) ([]byte, error) {
	if c == nil {
		return nil, errors.New("nil catalog row")
	}
	body := CatalogCanonical{
		Provider:              c.Provider,
		Model:                 c.Model,
		Mode:                  c.Mode,
		InputCostPerToken:     c.InputCostPerToken,
		OutputCostPerToken:    c.OutputCostPerToken,
		CacheReadCostPerToken: c.CacheReadCostPerToken,
		InputCostPerImage:     c.InputCostPerImage,
		InputCostPerCharacter: c.InputCostPerCharacter,
		InputCostPerSecond:    c.InputCostPerSecond,
		MaxInputTokens:        c.MaxInputTokens,
		MaxOutputTokens:       c.MaxOutputTokens,
		SizePricing:           c.SizePricing,
		EffectiveFrom:         c.EffectiveFrom.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
	}
	return json.Marshal(body)
}

// SignCatalogRow fills row.SignedAt / Signature / SignedByKeyID using
// the supplied private key. The same canonical bytes are used at
// verification time.
func SignCatalogRow(priv ed25519.PrivateKey, row *models.OperatorPriceCatalog) error {
	if priv == nil {
		return errors.New("nil signing key")
	}
	if row.EffectiveFrom.IsZero() {
		row.EffectiveFrom = time.Now().UTC()
	}
	row.EffectiveFrom = row.EffectiveFrom.UTC().Truncate(time.Microsecond)

	body, err := CanonicalCatalogBytes(row)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(priv, body)
	pub := priv.Public().(ed25519.PublicKey)
	keyID := hex.EncodeToString(sha256.New().Sum(pub))[:16]

	row.Signature = base64.StdEncoding.EncodeToString(sig)
	row.SignedByKeyID = keyID
	now := time.Now().UTC()
	row.SignedAt = &now
	return nil
}

// VerifyCatalogRow returns nil iff the signature in row was produced by
// pub over the canonical bytes of row. An unsigned row is treated as
// invalid for billing-truth purposes; the engine will fall back to the
// embedded JSON when verification fails.
func VerifyCatalogRow(pub ed25519.PublicKey, row *models.OperatorPriceCatalog) error {
	if row.Signature == "" {
		return errors.New("row is unsigned")
	}
	sig, err := base64.StdEncoding.DecodeString(row.Signature)
	if err != nil {
		return fmt.Errorf("decode sig: %w", err)
	}
	body, err := CanonicalCatalogBytes(row)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, body, sig) {
		return errors.New("ed25519: signature mismatch")
	}
	return nil
}

// ---- engine plumbing ------------------------------------------------------

// CatalogStore is the slice of *db.DB methods cost.Engine needs.
// Defined here as an interface to avoid import cycles with the db
// package (db imports models, models imports nothing — engine can keep
// using db.DB directly via a typed assertion in server wiring).
type CatalogStore interface {
	ListActiveOperatorCatalog(ctx context.Context) ([]models.OperatorPriceCatalog, error)
	ListActiveOrgDiscounts(ctx context.Context) ([]models.OrgProviderDiscount, error)
}

// catalogState is the moat-side of cost.Engine state. Kept on the
// Engine struct in engine.go via embedded fields below.
type catalogState struct {
	mu             sync.RWMutex
	operator       map[string]*ModelPricing
	loaded         bool
	pubKey         ed25519.PublicKey
	failedVerify   int
	loadedAt       time.Time
	// orgDiscounts is keyed by orgID-string (or "" for null org) and
	// then by provider name. discount_pct is in [0,1).
	orgDiscounts map[string]map[string]float64
}

// SetOperatorPubKey installs the verifier key. Must be called before
// LoadOperatorCatalog or every row will be rejected.
func (e *Engine) SetOperatorPubKey(pub ed25519.PublicKey) {
	e.cat.mu.Lock()
	defer e.cat.mu.Unlock()
	e.cat.pubKey = pub
}

// LoadOperatorCatalog reads operator_price_catalog and verifies every
// active row's Ed25519 signature. Verified rows replace the prior
// operator map atomically. Unverified rows are logged and ignored —
// GetPricing falls through to the embedded JSON for those, so the
// system stays available during a key rotation gone wrong.
func (e *Engine) LoadOperatorCatalog(ctx context.Context, store CatalogStore) error {
	if store == nil {
		return nil
	}
	rows, err := store.ListActiveOperatorCatalog(ctx)
	if err != nil {
		return err
	}

	e.cat.mu.RLock()
	pub := e.cat.pubKey
	e.cat.mu.RUnlock()

	next := make(map[string]*ModelPricing, len(rows))
	failed := 0
	for i := range rows {
		row := rows[i]
		if pub != nil {
			if err := VerifyCatalogRow(pub, &row); err != nil {
				if e.logger != nil {
					e.logger.Warn("operator catalog row failed verification",
						zap.String("provider", row.Provider),
						zap.String("model", row.Model),
						zap.Error(err))
				}
				failed++
				continue
			}
		}
		mp := &ModelPricing{
			Provider:           row.Provider,
			Model:              row.Model,
			Mode:               row.Mode,
			InputCostPerToken:  row.InputCostPerToken,
			OutputCostPerToken: row.OutputCostPerToken,
			SizePricing:        row.SizePricing,
		}
		if row.CacheReadCostPerToken != nil {
			mp.CacheReadCostPerToken = *row.CacheReadCostPerToken
		}
		if row.InputCostPerImage != nil {
			mp.InputCostPerImage = *row.InputCostPerImage
		}
		if row.InputCostPerCharacter != nil {
			mp.InputCostPerCharacter = *row.InputCostPerCharacter
		}
		if row.InputCostPerSecond != nil {
			mp.InputCostPerSecond = *row.InputCostPerSecond
		}
		if row.MaxInputTokens != nil {
			mp.MaxInputTokens = *row.MaxInputTokens
		}
		if row.MaxOutputTokens != nil {
			mp.MaxOutputTokens = *row.MaxOutputTokens
		}
		next[row.Provider+"/"+row.Model] = mp
	}

	e.cat.mu.Lock()
	e.cat.operator = next
	e.cat.failedVerify = failed
	e.cat.loaded = true
	e.cat.loadedAt = time.Now()
	e.cat.mu.Unlock()

	if e.logger != nil {
		e.logger.Info("operator catalog loaded",
			zap.Int("verified", len(next)),
			zap.Int("rejected", failed))
	}
	return nil
}

// LoadEffectiveDiscounts reads countersigned discounts and refreshes
// the in-memory cache used by EffectiveRate.
func (e *Engine) LoadEffectiveDiscounts(ctx context.Context, store CatalogStore) error {
	if store == nil {
		return nil
	}
	rows, err := store.ListActiveOrgDiscounts(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]map[string]float64)
	for _, row := range rows {
		// Only countersigned discounts affect billing.
		if row.AttestedAt == nil {
			continue
		}
		var k string
		if row.OrgID != nil {
			k = row.OrgID.String()
		}
		if _, ok := next[k]; !ok {
			next[k] = make(map[string]float64)
		}
		next[k][row.Provider] = row.DiscountPct
	}

	e.cat.mu.Lock()
	e.cat.orgDiscounts = next
	e.cat.mu.Unlock()
	if e.logger != nil {
		e.logger.Info("effective discounts loaded", zap.Int("orgs_with_discounts", len(next)))
	}
	return nil
}

// EffectiveRate returns the input/output per-token cost for the (org,
// provider, model) tuple, applying the org's countersigned discount on
// top of the operator catalog rate. Falls back to the catalog rate
// (no discount) when no discount applies. Returns false if pricing is
// unknown for the (provider, model) pair.
func (e *Engine) EffectiveRate(orgID, provider, model string) (input, output, discountPct float64, ok bool) {
	p := e.GetPricing(provider, model)
	if p == nil {
		return 0, 0, 0, false
	}
	d := e.discountFor(orgID, provider)
	mul := 1.0 - d
	return p.InputCostPerToken * mul, p.OutputCostPerToken * mul, d, true
}

// discountFor returns the active discount in [0,1) for (org, provider).
func (e *Engine) discountFor(orgID, provider string) float64 {
	e.cat.mu.RLock()
	defer e.cat.mu.RUnlock()
	if e.cat.orgDiscounts == nil {
		return 0
	}
	if m, ok := e.cat.orgDiscounts[orgID]; ok {
		if d, ok2 := m[provider]; ok2 {
			return d
		}
	}
	// Global / null-org discount fallback (single-tenant deploys).
	if m, ok := e.cat.orgDiscounts[""]; ok {
		if d, ok2 := m[provider]; ok2 {
			return d
		}
	}
	return 0
}

// BaselineCost computes what the request would have cost on the named
// baseline provider/model at the org's effective rate. Used by the
// savings ledger writer.
func (e *Engine) BaselineCost(orgID string, provider, model string, usage UsageInfo) (cost float64, discountPct float64, ok bool) {
	in, out, d, found := e.EffectiveRate(orgID, provider, model)
	if !found {
		return 0, 0, false
	}
	c := float64(usage.PromptTokens)*in + float64(usage.CompletionTokens)*out
	if usage.CachedTokens > 0 {
		// Honor cache discount on the baseline pricing entry. Falls
		// through to no-op when unset.
		p := e.GetPricing(provider, model)
		if p != nil && p.CacheReadCostPerToken > 0 {
			savings := float64(usage.CachedTokens) * (p.InputCostPerToken - p.CacheReadCostPerToken)
			c -= savings * (1 - d)
		}
	}
	if c < 0 {
		c = 0
	}
	return c, d, true
}

// CatalogStats reports verification counts for /metrics + admin UI.
func (e *Engine) CatalogStats() (verified, rejected int, loadedAt time.Time, hasPubKey bool) {
	e.cat.mu.RLock()
	defer e.cat.mu.RUnlock()
	return len(e.cat.operator), e.cat.failedVerify, e.cat.loadedAt, e.cat.pubKey != nil
}
