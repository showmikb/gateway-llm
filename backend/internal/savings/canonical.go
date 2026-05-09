// Package savings implements the smart-routing moat ledger: it
// computes baseline-vs-actual cost per request, signs the result with
// the operator's Ed25519 key, writes a row to routing_savings, and
// rolls daily aggregates into daily_savings.
//
// The package's defining property is verifiability: every row carries
// an Ed25519 signature over a canonical projection of its math fields,
// plus a hash chain (prev_hash) per-org. With just the operator's
// public key an auditor can replay the file and confirm no row was
// altered or removed.
package savings

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/models"
)

// Canonical is the deterministic projection of a routing_savings row
// used for signing and verification. We intentionally omit envelope
// fields (id, created_at, signature, signed_at, signed_by_key_id) — an
// auditor reconstructs them per-row and checks Sign(canonical) ==
// signature.
//
// We DO include prev_hash so the chain links are signed; flipping any
// preceding row changes the canonical bytes and breaks the chain.
type Canonical struct {
	RequestedAlias       string   `json:"requested_alias"`
	ServedAlias          string   `json:"served_alias"`
	ServedProvider       string   `json:"served_provider,omitempty"`
	ServedModel          string   `json:"served_model,omitempty"`
	BaselineProvider     string   `json:"baseline_provider,omitempty"`
	BaselineModel        string   `json:"baseline_model,omitempty"`
	BaselineInputTokens  int      `json:"baseline_input_tokens"`
	BaselineOutputTokens int      `json:"baseline_output_tokens"`
	ServedInputTokens    int      `json:"served_input_tokens"`
	ServedOutputTokens   int      `json:"served_output_tokens"`
	BaselineCostUSD      float64  `json:"baseline_cost_usd"`
	ActualCostUSD        float64  `json:"actual_cost_usd"`
	DiscountPctApplied   float64  `json:"discount_pct_applied"`
	SavingsUSD           float64  `json:"savings_usd"`
	QualityScore         *float64 `json:"quality_score,omitempty"`
	QualityPass          *bool    `json:"quality_pass,omitempty"`
	Strategy             string   `json:"strategy,omitempty"`
	Retried              bool     `json:"retried"`
	Overridden           bool     `json:"overridden"`
	OrgID                string   `json:"org_id,omitempty"`
	TraceID              string   `json:"trace_id,omitempty"`
	PrevHash             string   `json:"prev_hash,omitempty"`
}

// CanonicalBytes returns the bytes that are signed for a savings row.
func CanonicalBytes(row *models.RoutingSavings) ([]byte, error) {
	if row == nil {
		return nil, errors.New("nil savings row")
	}
	c := Canonical{
		RequestedAlias:       row.RequestedAlias,
		ServedAlias:          row.ServedAlias,
		ServedProvider:       row.ServedProvider,
		ServedModel:          row.ServedModel,
		BaselineProvider:     row.BaselineProvider,
		BaselineModel:        row.BaselineModel,
		BaselineInputTokens:  row.BaselineInputTokens,
		BaselineOutputTokens: row.BaselineOutputTokens,
		ServedInputTokens:    row.ServedInputTokens,
		ServedOutputTokens:   row.ServedOutputTokens,
		BaselineCostUSD:      row.BaselineCostUSD,
		ActualCostUSD:        row.ActualCostUSD,
		DiscountPctApplied:   row.DiscountPctApplied,
		SavingsUSD:           row.SavingsUSD,
		QualityScore:         row.QualityScore,
		QualityPass:          row.QualityPass,
		Strategy:             row.Strategy,
		Retried:              row.Retried,
		Overridden:           row.Overridden,
		TraceID:              row.TraceID,
		PrevHash:             row.PrevHash,
	}
	if row.OrgID != nil {
		c.OrgID = row.OrgID.String()
	}
	return json.Marshal(c)
}

// SignRow stamps signature, signed_at, and signed_by_key_id on the row.
func SignRow(priv ed25519.PrivateKey, row *models.RoutingSavings) error {
	if priv == nil {
		return errors.New("nil signing key")
	}
	body, err := CanonicalBytes(row)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(priv, body)
	pub := priv.Public().(ed25519.PublicKey)
	keyID := hex.EncodeToString(sha256.New().Sum(pub))[:16]
	now := time.Now().UTC()
	row.Signature = base64.StdEncoding.EncodeToString(sig)
	row.SignedByKeyID = keyID
	row.SignedAt = &now
	return nil
}

// VerifyRow returns nil iff the row's signature is a valid Ed25519
// signature by pub over the canonical bytes. Used by the
// /v1/management/savings/{id}/verify endpoint.
func VerifyRow(pub ed25519.PublicKey, row *models.RoutingSavings) error {
	if row.Signature == "" {
		return errors.New("row is unsigned")
	}
	sig, err := base64.StdEncoding.DecodeString(row.Signature)
	if err != nil {
		return err
	}
	body, err := CanonicalBytes(row)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, body, sig) {
		return errors.New("ed25519: signature mismatch")
	}
	return nil
}

// HashOf returns the hex-encoded SHA-256 of the canonical bytes; used
// as the prev_hash link for the next row in the same org's chain.
func HashOf(row *models.RoutingSavings) (string, error) {
	body, err := CanonicalBytes(row)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
