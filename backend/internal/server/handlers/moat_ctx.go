package handlers

import (
	"context"

	"github.com/gateway-llm/gateway-llm/internal/callbacks"
)

// moatCtxKey is private; only the chat handler stashes data and only
// helpers.go reads it back when dispatching the callbacks.RequestEvent.
type moatCtxKey struct{}

// WithMoatEvent stashes the smartroute / cost / quality / receipt
// bundle so logSpendAsyncCtx can promote it onto the dispatched event
// without threading every field through the existing log function
// signature. Cheap; the bundle is one tiny struct.
func WithMoatEvent(ctx context.Context, orgID string, routing *callbacks.RoutingEvent, costE *callbacks.CostEvent, quality *callbacks.QualityEvent, receipt *callbacks.ReceiptEvent) context.Context {
	return context.WithValue(ctx, moatCtxKey{}, &moatBundle{
		OrgID:   orgID,
		Routing: routing,
		Cost:    costE,
		Quality: quality,
		Receipt: receipt,
	})
}

type moatBundle struct {
	OrgID   string
	Routing *callbacks.RoutingEvent
	Cost    *callbacks.CostEvent
	Quality *callbacks.QualityEvent
	Receipt *callbacks.ReceiptEvent
}

func moatFromCtx(ctx context.Context) *moatBundle {
	v, _ := ctx.Value(moatCtxKey{}).(*moatBundle)
	return v
}
