package smartroute

import (
	"context"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ShadowRunner records side-by-side results from shadow (challenger)
// runs so they can be compared to the primary model's output. Actual
// calls to the challenger are launched in background goroutines by the
// handler; this runner only tracks outcomes and enforces a rate cap.
//
// ShadowRunner is deliberately decoupled from any HTTP layer so it can
// be unit-tested and later re-targeted at eval jobs.
type ShadowRunner struct {
	maxConcurrent int64
	inflight      atomic.Int64
	launched      atomic.Uint64
	completed     atomic.Uint64
	wins          atomic.Uint64
	losses        atomic.Uint64
	logger        *zap.Logger
}

func NewShadowRunner(maxConcurrent int, logger *zap.Logger) *ShadowRunner {
	if maxConcurrent <= 0 {
		maxConcurrent = 8
	}
	return &ShadowRunner{maxConcurrent: int64(maxConcurrent), logger: logger}
}

// ShadowResult is recorded after a background challenger finishes.
type ShadowResult struct {
	Winner       string
	PrimaryModel string
	ShadowModel  string
	PrimaryCost  float64
	ShadowCost   float64
	PrimaryMs    int64
	ShadowMs     int64
	RecordedAt   time.Time
}

// Acquire returns a release function if the runner has capacity. A nil
// release means the caller should not launch a shadow run this time.
func (s *ShadowRunner) Acquire() func() {
	if s == nil {
		return nil
	}
	if s.inflight.Add(1) > s.maxConcurrent {
		s.inflight.Add(-1)
		return nil
	}
	s.launched.Add(1)
	return func() { s.inflight.Add(-1) }
}

// Observe records the result of a completed shadow run. Callers pass
// "primary" or "shadow" for winner, or "" if inconclusive.
func (s *ShadowRunner) Observe(ctx context.Context, res ShadowResult) {
	if s == nil {
		return
	}
	s.completed.Add(1)
	switch res.Winner {
	case "shadow":
		s.wins.Add(1)
	case "primary":
		s.losses.Add(1)
	}
	if s.logger != nil {
		s.logger.Info("shadow result",
			zap.String("winner", res.Winner),
			zap.String("primary", res.PrimaryModel),
			zap.String("shadow", res.ShadowModel),
			zap.Float64("primary_cost", res.PrimaryCost),
			zap.Float64("shadow_cost", res.ShadowCost),
			zap.Int64("primary_ms", res.PrimaryMs),
			zap.Int64("shadow_ms", res.ShadowMs),
		)
	}
	_ = ctx
}

// ShadowStats mirrors the runner's counters for /v1/metrics output.
type ShadowStats struct {
	Launched  uint64 `json:"launched"`
	Completed uint64 `json:"completed"`
	Wins      uint64 `json:"shadow_wins"`
	Losses    uint64 `json:"primary_wins"`
}

func (s *ShadowRunner) Stats() ShadowStats {
	if s == nil {
		return ShadowStats{}
	}
	return ShadowStats{
		Launched:  s.launched.Load(),
		Completed: s.completed.Load(),
		Wins:      s.wins.Load(),
		Losses:    s.losses.Load(),
	}
}
