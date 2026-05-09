package router

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SpeculativeResult is what ExecuteSpeculative returns when one of the
// two branches wins (or both fail). Winner is "cheap" or "expensive" so
// the caller can attribute savings in the spend log / recording.
type SpeculativeResult struct {
	Response *http.Response
	Dep      *DeploymentInfo
	Winner   string
	// CheapElapsed / ExpensiveElapsed are wall-clock times for each
	// branch. Useful for the savings dashboard to learn which tasks
	// the cheap model is already fast enough for.
	CheapElapsed     time.Duration
	ExpensiveElapsed time.Duration
}

// ExecuteSpeculative fires `cheap` and `expensive` aliases in parallel
// and returns the first non-error response. The loser is cancelled and
// its body drained. This is the speculative-execution primitive called
// out in Pillar 2 of the moat plan: for the <1% of requests where the
// classifier is uncertain, we race both tiers so the user sees the
// cheaper latency when the cheap model can actually answer.
//
// If CheapGrace is non-zero the expensive branch is delayed by that
// duration — long enough for the cheap branch to typically win when it
// can answer quickly, short enough to mask frontier-model latency when
// it cannot. Sensible default: 250ms.
//
// If both branches fail the last error is returned with a combined
// message so the handler can surface a helpful 502.
func (r *ModelRouter) ExecuteSpeculative(
	ctx context.Context,
	orgID *uuid.UUID,
	cheapAlias, expensiveAlias string,
	cheapGrace time.Duration,
	fn RequestFunc,
) (*SpeculativeResult, error) {
	if cheapAlias == "" || expensiveAlias == "" || cheapAlias == expensiveAlias {
		// Nothing to speculate — fall back to the plain path against
		// whichever alias is non-empty.
		alias := cheapAlias
		if alias == "" {
			alias = expensiveAlias
		}
		resp, dep, err := r.ExecuteWithFallbackForOrg(ctx, orgID, alias, fn)
		if err != nil {
			return nil, err
		}
		return &SpeculativeResult{Response: resp, Dep: dep, Winner: "cheap"}, nil
	}

	parentCtx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()

	type branchResult struct {
		resp    *http.Response
		dep     *DeploymentInfo
		err     error
		elapsed time.Duration
		name    string
	}
	results := make(chan branchResult, 2)

	launch := func(name, alias string, delay time.Duration) {
		go func() {
			if delay > 0 {
				select {
				case <-parentCtx.Done():
					results <- branchResult{err: parentCtx.Err(), name: name}
					return
				case <-time.After(delay):
				}
			}
			start := time.Now()
			resp, dep, err := r.ExecuteWithFallbackForOrg(parentCtx, orgID, alias, fn)
			results <- branchResult{resp: resp, dep: dep, err: err, elapsed: time.Since(start), name: name}
		}()
	}

	launch("cheap", cheapAlias, 0)
	launch("expensive", expensiveAlias, cheapGrace)

	// Collect the first non-error, then drain + discard the loser.
	var winner *branchResult
	var losers []branchResult
	var lastErr error
	for i := 0; i < 2; i++ {
		br := <-results
		if br.err != nil {
			lastErr = br.err
			losers = append(losers, br)
			continue
		}
		if winner == nil {
			winner = &br
			// Cancel the sibling. Its goroutine will still publish a
			// result so we don't leak it; we'll drain on the next loop.
			cancelAll()
			continue
		}
		// We already have a winner; treat this as the loser.
		losers = append(losers, br)
	}

	for _, l := range losers {
		if l.resp != nil {
			_ = l.resp.Body.Close()
		}
	}

	if winner == nil {
		if lastErr == nil {
			lastErr = errors.New("speculative: both branches returned no response")
		}
		r.logger.Warn("speculative exec failed", zap.Error(lastErr))
		return nil, lastErr
	}

	sr := &SpeculativeResult{
		Response: winner.resp,
		Dep:      winner.dep,
		Winner:   winner.name,
	}
	if winner.name == "cheap" {
		sr.CheapElapsed = winner.elapsed
	} else {
		sr.ExpensiveElapsed = winner.elapsed
	}
	for _, l := range losers {
		if l.name == "cheap" {
			sr.CheapElapsed = l.elapsed
		} else {
			sr.ExpensiveElapsed = l.elapsed
		}
	}

	r.logger.Debug("speculative exec picked winner",
		zap.String("winner", sr.Winner),
		zap.Duration("cheap_ms", sr.CheapElapsed),
		zap.Duration("expensive_ms", sr.ExpensiveElapsed),
	)
	return sr, nil
}

// speculativeWG is reused by tests to ensure branch goroutines finish
// before asserting on metrics. Exported only under _test.go.
var speculativeWG sync.WaitGroup //nolint:unused
