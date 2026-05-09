package models

import (
	"time"

	"github.com/google/uuid"
)

// OperatorPriceCatalog is one row in the operator-signed price catalog.
// Once signed_at is non-nil and signature is verified at startup, this
// row becomes part of the immutable billing-truth that org admins
// cannot change. effective_to is set when a row is superseded so
// historical receipts and savings entries can still be re-priced
// against the rate that was in force at the time.
type OperatorPriceCatalog struct {
	ID                    uuid.UUID          `json:"id"`
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
	Source                string             `json:"source"`
	EffectiveFrom         time.Time          `json:"effective_from"`
	EffectiveTo           *time.Time         `json:"effective_to,omitempty"`
	SignedAt              *time.Time         `json:"signed_at,omitempty"`
	Signature             string             `json:"signature,omitempty"`
	SignedByKeyID         string             `json:"signed_by_key_id,omitempty"`
	CreatedAt             time.Time          `json:"created_at"`
}

// OrgProviderDiscount captures a customer's negotiated discount with a
// provider. It is "declared" by the org admin and only takes effect
// once the operator countersigns it (attested_at non-nil + valid
// operator_signature). The discount_pct is applied multiplicatively to
// the operator catalog rate when computing baseline_cost / actual_cost.
type OrgProviderDiscount struct {
	ID                 uuid.UUID  `json:"id"`
	OrgID              *uuid.UUID `json:"org_id,omitempty"`
	Provider           string     `json:"provider"`
	DiscountPct        float64    `json:"discount_pct"`
	EvidenceURL        *string    `json:"evidence_url,omitempty"`
	EvidenceNote       *string    `json:"evidence_note,omitempty"`
	DeclaredBy         *uuid.UUID `json:"declared_by,omitempty"`
	DeclaredAt         time.Time  `json:"declared_at"`
	AttestedBy         *uuid.UUID `json:"attested_by,omitempty"`
	AttestedAt         *time.Time `json:"attested_at,omitempty"`
	OperatorSignature  *string    `json:"operator_signature,omitempty"`
	EffectiveFrom      time.Time  `json:"effective_from"`
	EffectiveTo        *time.Time `json:"effective_to,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

// Status is a derived field for the UI; not stored.
func (d OrgProviderDiscount) Status() string {
	if d.EffectiveTo != nil && d.EffectiveTo.Before(time.Now()) {
		return "expired"
	}
	if d.AttestedAt != nil {
		return "countersigned"
	}
	return "declared"
}

// RoutingPolicy is the per-alias (and optionally per-org) configuration
// for the smart-router quality safety net. See the migration for the
// allowed strategies.
type RoutingPolicy struct {
	ID                       uuid.UUID  `json:"id"`
	ModelAlias               string     `json:"model_alias"`
	OrgID                    *uuid.UUID `json:"org_id,omitempty"`
	Strategy                 string     `json:"strategy"`
	QualityThreshold         float64    `json:"quality_threshold"`
	BaselineProviderModel    *string    `json:"baseline_provider_model,omitempty"`
	JudgeAlias               *string    `json:"judge_alias,omitempty"`
	RetryWhenStreaming       bool       `json:"retry_when_streaming"`
	SamplePct                int        `json:"sample_pct"`
	MinSamplesBeforeRouting  int        `json:"min_samples_before_routing"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

// RoutingSavings is one signed ledger entry. The signature covers a
// canonical projection of the math fields so an auditor armed with
// just the operator pubkey can confirm the row was not altered.
type RoutingSavings struct {
	ID                    uuid.UUID  `json:"id"`
	RecordingID           *uuid.UUID `json:"recording_id,omitempty"`
	TraceID               string     `json:"trace_id,omitempty"`
	OrgID                 *uuid.UUID `json:"org_id,omitempty"`
	APIKeyID              *uuid.UUID `json:"api_key_id,omitempty"`
	RequestedAlias        string     `json:"requested_alias"`
	ServedAlias           string     `json:"served_alias"`
	ServedProvider        string     `json:"served_provider,omitempty"`
	ServedModel           string     `json:"served_model,omitempty"`
	BaselineProvider      string     `json:"baseline_provider,omitempty"`
	BaselineModel         string     `json:"baseline_model,omitempty"`
	BaselineInputTokens   int        `json:"baseline_input_tokens"`
	BaselineOutputTokens  int        `json:"baseline_output_tokens"`
	ServedInputTokens     int        `json:"served_input_tokens"`
	ServedOutputTokens    int        `json:"served_output_tokens"`
	BaselineCostUSD       float64    `json:"baseline_cost_usd"`
	ActualCostUSD         float64    `json:"actual_cost_usd"`
	DiscountPctApplied    float64    `json:"discount_pct_applied"`
	SavingsUSD            float64    `json:"savings_usd"`
	QualityScore          *float64   `json:"quality_score,omitempty"`
	QualityPass           *bool      `json:"quality_pass,omitempty"`
	QualityScorer         string     `json:"quality_scorer,omitempty"`
	Retried               bool       `json:"retried"`
	Strategy              string     `json:"strategy,omitempty"`
	ComplexityScore       *float64   `json:"complexity_score,omitempty"`
	ComplexityBucket      string     `json:"complexity_bucket,omitempty"`
	Overridden            bool       `json:"overridden"`
	PrevHash              string     `json:"prev_hash,omitempty"`
	Signature             string     `json:"signature,omitempty"`
	SignedByKeyID         string     `json:"signed_by_key_id,omitempty"`
	SignedAt              *time.Time `json:"signed_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
}

// DailySavings is the rollup the dashboard hero stats render from.
type DailySavings struct {
	ID                    uuid.UUID  `json:"id"`
	Date                  time.Time  `json:"date"`
	OrgID                 *uuid.UUID `json:"org_id,omitempty"`
	TotalRequests         int        `json:"total_requests"`
	RoutedRequests        int        `json:"routed_requests"`
	RetryCount            int        `json:"retry_count"`
	TotalBaselineCostUSD  float64    `json:"total_baseline_cost_usd"`
	TotalActualCostUSD    float64    `json:"total_actual_cost_usd"`
	TotalSavingsUSD       float64    `json:"total_savings_usd"`
	OurCutUSD             float64    `json:"our_cut_usd"`
	QualityPassPct        *float64   `json:"quality_pass_pct,omitempty"`
	AvgQualityScore       *float64   `json:"avg_quality_score,omitempty"`
	ComputedAt            time.Time  `json:"computed_at"`
}
