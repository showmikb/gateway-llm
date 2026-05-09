package savings

import (
	"context"
	"crypto/ed25519"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/models"
)

// Store is the slice of *db.DB methods Writer needs. Defined as an
// interface so the writer can be tested without a live Postgres.
type Store interface {
	InsertRoutingSavings(ctx context.Context, s *models.RoutingSavings) error
	PrevSavingsHashForOrg(ctx context.Context, orgID *uuid.UUID) (string, error)
}

// Writer assembles a routing_savings row from a routing decision +
// cost engine view of the request and persists it. All math uses
// the operator catalog rate (cost.Engine.EffectiveRate) so customers
// cannot influence the savings number; only signed catalog + signed
// discount declarations count.
type Writer struct {
	cost   *cost.Engine
	store  Store
	signer ed25519.PrivateKey
	logger *zap.Logger

	// chainMu serializes prev_hash lookup + insert per org so two
	// concurrent requests can't both read the same prev_hash and
	// produce a chain fork. Cheap; the critical section is one
	// SELECT + one INSERT.
	chainMu sync.Mutex
}

func NewWriter(costEng *cost.Engine, store Store, signer ed25519.PrivateKey, logger *zap.Logger) *Writer {
	return &Writer{cost: costEng, store: store, signer: signer, logger: logger}
}

// Decision captures everything Writer needs from the chat handler.
// Producers fill in what they have; missing baseline_provider_model
// (because the policy didn't name one) collapses to served_alias's
// pricing and savings_usd will be 0.
type Decision struct {
	OrgID            *uuid.UUID
	APIKeyID         *uuid.UUID
	RecordingID      *uuid.UUID
	TraceID          string
	RequestedAlias   string
	ServedAlias      string
	ServedProvider   string
	ServedModel      string
	BaselineProvider string
	BaselineModel    string
	Usage            cost.UsageInfo
	BaselineUsage    *cost.UsageInfo // nil → assume same usage as served (closest counterfactual we have)
	ActualCostUSD    float64         // already computed by chat handler
	Strategy         string
	ComplexityScore  *float64
	ComplexityBucket string
	Overridden       bool
	Retried          bool
	QualityScore     *float64
	QualityPass      *bool
	QualityScorer    string
}

// Write computes baseline / actual / savings, signs the row, and
// inserts it. Returns the signed row (ID populated) so the caller can
// stamp it onto receipts and metrics.
func (w *Writer) Write(ctx context.Context, d Decision) (*models.RoutingSavings, error) {
	if d.RequestedAlias == "" {
		d.RequestedAlias = d.ServedAlias
	}
	row := &models.RoutingSavings{
		RecordingID:      d.RecordingID,
		TraceID:          d.TraceID,
		OrgID:            d.OrgID,
		APIKeyID:         d.APIKeyID,
		RequestedAlias:   d.RequestedAlias,
		ServedAlias:      d.ServedAlias,
		ServedProvider:   d.ServedProvider,
		ServedModel:      d.ServedModel,
		BaselineProvider: d.BaselineProvider,
		BaselineModel:    d.BaselineModel,
		ActualCostUSD:    d.ActualCostUSD,
		Retried:          d.Retried,
		Overridden:       d.Overridden,
		Strategy:         d.Strategy,
		ComplexityScore:  d.ComplexityScore,
		ComplexityBucket: d.ComplexityBucket,
		QualityScore:     d.QualityScore,
		QualityPass:      d.QualityPass,
		QualityScorer:    d.QualityScorer,
	}

	row.ServedInputTokens = d.Usage.PromptTokens
	row.ServedOutputTokens = d.Usage.CompletionTokens

	baselineUsage := d.Usage
	if d.BaselineUsage != nil {
		baselineUsage = *d.BaselineUsage
	}
	row.BaselineInputTokens = baselineUsage.PromptTokens
	row.BaselineOutputTokens = baselineUsage.CompletionTokens

	orgIDStr := ""
	if d.OrgID != nil {
		orgIDStr = d.OrgID.String()
	}

	// Baseline cost: prefer the explicit baseline_provider_model from
	// the routing policy. If absent, fall back to the requested alias
	// itself (i.e. the Decision's served path is the baseline; savings
	// will be 0 unless we overrode).
	baseProv := strings.TrimSpace(d.BaselineProvider)
	baseModel := strings.TrimSpace(d.BaselineModel)
	if (baseProv == "" || baseModel == "") && d.ServedProvider != "" && d.ServedModel != "" {
		// No declared baseline: treat the served path as baseline,
		// which makes savings_usd == 0. We still emit the row for
		// auditability.
		baseProv, baseModel = d.ServedProvider, d.ServedModel
	}

	if w.cost != nil && baseProv != "" && baseModel != "" {
		baseCost, discount, ok := w.cost.BaselineCost(orgIDStr, baseProv, baseModel, baselineUsage)
		if ok {
			row.BaselineCostUSD = baseCost
			row.DiscountPctApplied = discount
		}
	}

	row.SavingsUSD = row.BaselineCostUSD - row.ActualCostUSD

	// Hash chain: serialize per-org so prev_hash is monotonic.
	w.chainMu.Lock()
	if w.store != nil {
		prev, err := w.store.PrevSavingsHashForOrg(ctx, d.OrgID)
		if err == nil {
			row.PrevHash = prev
		}
	}
	if w.signer != nil {
		if err := SignRow(w.signer, row); err != nil {
			w.chainMu.Unlock()
			if w.logger != nil {
				w.logger.Warn("savings sign failed", zap.Error(err))
			}
			return nil, err
		}
	}
	if w.store != nil {
		if err := w.store.InsertRoutingSavings(ctx, row); err != nil {
			w.chainMu.Unlock()
			return nil, err
		}
	}
	w.chainMu.Unlock()

	if w.logger != nil {
		w.logger.Debug("savings row written",
			zap.String("trace_id", d.TraceID),
			zap.String("requested_alias", row.RequestedAlias),
			zap.String("served_alias", row.ServedAlias),
			zap.Float64("baseline_usd", row.BaselineCostUSD),
			zap.Float64("actual_usd", row.ActualCostUSD),
			zap.Float64("savings_usd", row.SavingsUSD))
	}

	return row, nil
}

// EnabledForOrg is a convenience for callers that want to skip ledger
// work when the operator catalog isn't loaded (no signing key, or
// pre-MVP install). Always true for now; here so we can flip it via
// config later without changing call sites.
func (w *Writer) EnabledForOrg(_ *uuid.UUID) bool {
	return w != nil
}

var _ = time.Now // keep time import for future fields without churn
