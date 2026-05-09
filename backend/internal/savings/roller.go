package savings

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/gateway-llm/gateway-llm/internal/models"
)

// RollerStore is the slice of *db.DB methods Roller needs.
type RollerStore interface {
	DistinctSavingsOrgsForDate(ctx context.Context, date time.Time) ([]*uuid.UUID, error)
	UpsertDailySavingsForDate(ctx context.Context, date time.Time, orgID *uuid.UUID, ourCutPct float64) (*models.DailySavings, error)
}

// Roller is the periodic worker that rolls routing_savings into
// daily_savings. Runs in-process; no separate cron required.
type Roller struct {
	store     RollerStore
	cutPct    float64
	interval  time.Duration
	logger    *zap.Logger
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewRoller schedules the roller. cutPct is the operator's % of
// savings (e.g. 0.20 = "we keep 20% of the savings we generated for
// you") used to populate daily_savings.our_cut_usd. interval=0 picks
// the default 5 minutes.
func NewRoller(store RollerStore, cutPct float64, interval time.Duration, logger *zap.Logger) *Roller {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &Roller{
		store:    store,
		cutPct:   cutPct,
		interval: interval,
		logger:   logger,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start runs the loop until ctx cancellation or Stop. Idempotent
// because UpsertDailySavingsForDate is keyed on (date, org_id).
func (r *Roller) Start(ctx context.Context) {
	if r == nil || r.store == nil {
		return
	}
	go func() {
		defer close(r.doneCh)
		// First run almost immediately so dashboard isn't empty after
		// startup; subsequent runs honour the interval.
		t := time.NewTimer(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			case <-t.C:
				r.runOnce(ctx)
				t.Reset(r.interval)
			}
		}
	}()
}

// Stop signals the loop and waits for it to exit. Safe to call
// multiple times; subsequent calls are no-ops.
func (r *Roller) Stop() {
	if r == nil {
		return
	}
	select {
	case <-r.stopCh:
		return
	default:
		close(r.stopCh)
	}
	<-r.doneCh
}

func (r *Roller) runOnce(ctx context.Context) {
	// Roll today and yesterday so a request that lands at 23:59 still
	// makes it into the previous day's bucket on the very next tick.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	yesterday := today.Add(-24 * time.Hour)
	for _, day := range []time.Time{yesterday, today} {
		orgs, err := r.store.DistinctSavingsOrgsForDate(ctx, day)
		if err != nil {
			if r.logger != nil {
				r.logger.Warn("roller: list orgs failed",
					zap.Time("date", day), zap.Error(err))
			}
			continue
		}
		for _, oid := range orgs {
			if _, err := r.store.UpsertDailySavingsForDate(ctx, day, oid, r.cutPct); err != nil && r.logger != nil {
				r.logger.Warn("roller: upsert daily failed",
					zap.Time("date", day), zap.Error(err))
			}
		}
	}
}
