package handlers

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/gateway-llm/gateway-llm/internal/callbacks"
	"github.com/gateway-llm/gateway-llm/internal/cost"
	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/router"
	"github.com/gateway-llm/gateway-llm/internal/savings"
	"github.com/gateway-llm/gateway-llm/internal/server/middleware"
	"github.com/gateway-llm/gateway-llm/internal/smartroute"
)

// lookupRoutingPolicy returns the per-(alias, org) policy if one
// exists, falling back to the global default for the alias. Result is
// nil when nothing is configured — callers default to "track_only" in
// that case so behaviour matches the pre-policy world.
//
// We swallow DB errors here: a transient DB hiccup must NOT take down
// the chat path. The caller proceeds with the default policy and the
// savings ledger writer logs the failure separately.
func (h *Handlers) lookupRoutingPolicy(ctx context.Context, alias string) *models.RoutingPolicy {
	if h.DB == nil || alias == "" {
		return nil
	}
	orgID := middleware.GetOrgID(ctx)
	// Per-org override first.
	if orgID != nil {
		if p, err := h.DB.GetRoutingPolicy(ctx, alias, orgID); err == nil && p != nil {
			return p
		}
	}
	// Global default (org_id NULL).
	if p, err := h.DB.GetRoutingPolicy(ctx, alias, nil); err == nil && p != nil {
		return p
	}
	return nil
}

type savingsLedgerInput struct {
	traceID        string
	key            *models.APIKey
	dep            *router.DeploymentInfo
	requestedAlias string
	servedAlias    string
	decision       smartroute.Decision
	policy         *models.RoutingPolicy
	strategy       string
	uinfo          cost.UsageInfo
	actualCost     float64
}

// writeSavingsLedger fans out a single routing_savings row write per
// completed request, off the hot path. The function tolerates missing
// dependencies (no Savings writer, no DB, no policy): callers still
// get a successful response, the moat just doesn't record the row.
//
// The strategy-driven safety net (auto_retry / judge_then_decide)
// requires inline judge calls that are not yet wired through the
// chat path; this writer therefore records the *configured* strategy
// from the routing_policy table so the dashboard reflects intent
// even before the inline judge ships. The savings_ledger pillar is
// independently useful — it proves how much was saved per request.
func (h *Handlers) writeSavingsLedger(ctx context.Context, in savingsLedgerInput) {
	if h.Savings == nil {
		return
	}
	orgID := middleware.GetOrgID(ctx)
	var keyID *uuid.UUID
	if in.key != nil && in.key.ID != uuid.Nil {
		id := in.key.ID
		keyID = &id
	}

	baselineProvider, baselineModel := splitBaselineProviderModel(in.policy)
	if baselineProvider == "" || baselineModel == "" {
		// Fall back to the requested alias's deployment via the router
		// pricing lookup. We need provider+model to compute baseline
		// cost; if no override fired and we can't resolve a baseline,
		// the writer collapses to served-as-baseline (savings = 0).
		if h.Router != nil && in.requestedAlias != "" {
			deps := h.Router.GetDeployments(in.requestedAlias)
			if len(deps) > 0 && deps[0] != nil {
				baselineProvider = deps[0].Provider
				baselineModel = deps[0].ProviderModel
			}
		}
	}

	overridden := false
	var complexityScore *float64
	complexityBucket := ""
	if in.decision.Enabled || in.decision.Overridden {
		s := in.decision.Score
		complexityScore = &s
		complexityBucket = string(in.decision.Bucket)
		overridden = in.decision.Overridden
	}

	d := savings.Decision{
		OrgID:            orgID,
		APIKeyID:         keyID,
		TraceID:          in.traceID,
		RequestedAlias:   in.requestedAlias,
		ServedAlias:      in.servedAlias,
		ServedProvider:   in.dep.Provider,
		ServedModel:      in.dep.ProviderModel,
		BaselineProvider: baselineProvider,
		BaselineModel:    baselineModel,
		Usage:            in.uinfo,
		ActualCostUSD:    in.actualCost,
		Strategy:         in.strategy,
		ComplexityScore:  complexityScore,
		ComplexityBucket: complexityBucket,
		Overridden:       overridden,
	}

	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := h.Savings.Write(bg, d); err != nil && h.Logger != nil {
			h.Logger.Debug("savings ledger write failed", zap.Error(err))
		}
	}()
}

// splitBaselineProviderModel parses the "provider/model" string the
// routing policy stores. Returns ("", "") when unset or malformed so
// callers fall back to alias-resolved deployment info.
func splitBaselineProviderModel(p *models.RoutingPolicy) (string, string) {
	if p == nil || p.BaselineProviderModel == nil {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimSpace(*p.BaselineProviderModel), "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// silence unused imports when these utilities are reached at compile
// time but call sites differ across builds.
var _ = db.SavingsFilter{}

// attachMoatCtx mirrors the savings ledger inputs into a
// callbacks-friendly bundle and stashes it on ctx so logSpendAsyncCtx
// can forward routing.* / cost.* / quality.* / receipt.* attributes
// to every dispatched event without the savings writer needing to
// run synchronously.
func (h *Handlers) attachMoatCtx(ctx context.Context, in savingsLedgerInput) context.Context {
	if h == nil {
		return ctx
	}
	routing := &callbacks.RoutingEvent{
		RequestedAlias: in.requestedAlias,
		ServedAlias:    in.servedAlias,
		Strategy:       in.strategy,
		Overridden:     in.decision.Overridden,
	}
	if in.decision.Enabled || in.decision.Overridden {
		routing.ComplexityScore = in.decision.Score
		routing.ComplexityBucket = string(in.decision.Bucket)
	}

	// Best-effort baseline cost recompute for the callback. Heavy
	// query path is the savings ledger writer; here we only need the
	// already-cached EffectiveRate result for the served path.
	costE := &callbacks.CostEvent{ActualUSD: in.actualCost}
	if h.CostEng != nil {
		baseProv, baseModel := splitBaselineProviderModel(in.policy)
		if (baseProv == "" || baseModel == "") && in.dep != nil {
			baseProv = in.dep.Provider
			baseModel = in.dep.ProviderModel
		}
		orgIDStr := ""
		if oid := middleware.GetOrgID(ctx); oid != nil {
			orgIDStr = oid.String()
		}
		if base, disc, ok := h.CostEng.BaselineCost(orgIDStr, baseProv, baseModel, in.uinfo); ok {
			costE.BaselineUSD = base
			costE.DiscountPct = disc
			costE.SavingsUSD = base - in.actualCost
		}
	}

	orgIDStr := ""
	if oid := middleware.GetOrgID(ctx); oid != nil {
		orgIDStr = oid.String()
	}
	return WithMoatEvent(ctx, orgIDStr, routing, costE, nil, nil)
}
