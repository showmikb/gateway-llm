package callbacks

import (
	"context"
	"time"
)

type RequestEvent struct {
	TraceID          string            `json:"trace_id"`
	SpanID           string            `json:"span_id"`
	Timestamp        time.Time         `json:"timestamp"`
	DurationMS       int64             `json:"duration_ms"`
	Method           string            `json:"method"`
	Endpoint         string            `json:"endpoint"`
	ModelAlias       string            `json:"model_alias"`
	Provider         string            `json:"provider"`
	ProviderModel    string            `json:"provider_model"`
	Status           int               `json:"status"`
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	TotalTokens      int               `json:"total_tokens"`
	CostUSD          float64           `json:"cost_usd"`
	APIKeyID         string            `json:"api_key_id,omitempty"`
	UserID           string            `json:"user_id,omitempty"`
	TeamID           string            `json:"team_id,omitempty"`
	OrgID            string            `json:"org_id,omitempty"`
	ErrorMessage     string            `json:"error,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`

	// Smart-routing moat fields. All optional — populated only when
	// the request flowed through the routing decision path. Carried as
	// first-class struct fields (not Metadata) so each downstream sink
	// can map them to its native attribute namespace without parsing
	// strings (OTEL: routing.*, cost.*, quality.*, receipt.*; Datadog:
	// gateway_llm.routing.*, etc.).
	Routing  *RoutingEvent  `json:"routing,omitempty"`
	Cost     *CostEvent     `json:"cost,omitempty"`
	Quality  *QualityEvent  `json:"quality,omitempty"`
	Receipt  *ReceiptEvent  `json:"receipt,omitempty"`
}

// RoutingEvent describes the smart-routing decision that produced the
// served response. Empty when smartroute was disabled for the request.
type RoutingEvent struct {
	RequestedAlias    string  `json:"requested_alias,omitempty"`
	ServedAlias       string  `json:"served_alias,omitempty"`
	Strategy          string  `json:"policy_strategy,omitempty"`
	ComplexityScore   float64 `json:"complexity_score,omitempty"`
	ComplexityBucket  string  `json:"complexity_bucket,omitempty"`
	Overridden        bool    `json:"overridden,omitempty"`
	Retried           bool    `json:"retried,omitempty"`
}

// CostEvent carries the full baseline-vs-actual cost picture for the
// request so downstream observability can dashboard $ saved without
// joining tables. DiscountPct is in [0,1).
type CostEvent struct {
	BaselineUSD     float64 `json:"baseline_usd,omitempty"`
	ActualUSD       float64 `json:"actual_usd,omitempty"`
	SavingsUSD      float64 `json:"savings_usd,omitempty"`
	DiscountPct     float64 `json:"discount_pct_applied,omitempty"`
}

// QualityEvent is populated when an inline or sampled judge produced a
// score for this request.
type QualityEvent struct {
	Score *float64 `json:"score,omitempty"`
	Pass  *bool    `json:"pass,omitempty"`
	Scorer string  `json:"scorer,omitempty"`
}

// ReceiptEvent surfaces the cryptographic ID + signature so verifiers
// downstream can fetch + audit without joining receipt tables.
type ReceiptEvent struct {
	ID            string `json:"id,omitempty"`
	Signature     string `json:"signature,omitempty"`
	SignedByKeyID string `json:"signed_by_key_id,omitempty"`
}

type Callback interface {
	Name() string
	Send(ctx context.Context, event RequestEvent) error
	Close() error
}
