// Package qualityeval is the online quality evaluator that closes the
// smart-routing learning loop. After a request is served the chat
// handler enqueues the (request, response, alias) tuple here; a
// background worker samples per-alias according to the routing policy,
// scores the cheap response with an LLMJudge call, writes the result
// to eval_scores, and emits a feedback_events row so the existing
// `gateway-llm train` command picks the new signal up on its next run.
//
// The evaluator is best-effort: queue-full drops, scorer failures, and
// missing recordings never propagate back to the chat hot path.
package qualityeval

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/gateway-llm/gateway-llm/internal/ir"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/replay"
)

// Store is the slice of *db.DB methods Runner needs. Keeping it as an
// interface prevents an import cycle and makes tests trivial.
type Store interface {
	GetRoutingPolicy(ctx context.Context, alias string, orgID *uuid.UUID) (*models.RoutingPolicy, error)
	InsertEvalScore(ctx context.Context, s *models.EvalScore) error
	InsertFeedback(ctx context.Context, ev *models.FeedbackEvent) error
}

// JudgeInvoker is the same callback type the existing replay.LLMJudge
// uses: given a model alias and a prompt, return the raw text reply.
// We keep the type local so callers don't need to import replay just
// to wire qualityeval up.
type JudgeInvoker func(ctx context.Context, model, prompt string) (string, error)

// Pending is the bundle the chat handler hands to Enqueue.
type Pending struct {
	TraceID        string
	OrgID          *uuid.UUID
	APIKeyID       *uuid.UUID
	RequestedAlias string  // the alias the customer asked for; used to look up policy
	ServedAlias    string
	RecordingID    *uuid.UUID
	Request        *ir.ChatRequest
	Response       *ir.ChatResponse
}

// Runner is a small fixed-pool worker. The chat handler calls Enqueue
// in a non-blocking way; when the queue is full we drop and bump a
// counter (Dropped) so operators can see when capacity is needed.
type Runner struct {
	store         Store
	invoke        JudgeInvoker
	defaultJudge  string // fallback alias when policy.judge_alias is empty
	logger        *zap.Logger
	queue         chan *Pending
	workers       int
	rand          *rand.Rand
	randMu        sync.Mutex
	wg            sync.WaitGroup
	dropped       atomic.Uint64
	stopped       atomic.Bool
}

// New constructs a Runner. defaultJudgeAlias is used when the
// per-alias routing policy doesn't specify one; an empty value
// disables the runner entirely (Enqueue becomes a no-op).
func New(store Store, invoke JudgeInvoker, defaultJudgeAlias string, logger *zap.Logger) *Runner {
	if logger == nil {
		logger = zap.NewNop()
	}
	r := &Runner{
		store:        store,
		invoke:       invoke,
		defaultJudge: defaultJudgeAlias,
		logger:       logger,
		queue:        make(chan *Pending, 1024),
		workers:      2,
		rand:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for i := 0; i < r.workers; i++ {
		r.wg.Add(1)
		go r.run()
	}
	return r
}

// Enqueue hands p to a worker. Non-blocking; full-queue drops are
// surfaced via Dropped() and a debug log line.
func (r *Runner) Enqueue(p *Pending) {
	if r == nil || r.stopped.Load() || p == nil {
		return
	}
	if r.invoke == nil {
		return
	}
	select {
	case r.queue <- p:
	default:
		r.dropped.Add(1)
	}
}

// Dropped returns the running count of pending evals dropped due to
// queue pressure since process start.
func (r *Runner) Dropped() uint64 { return r.dropped.Load() }

// Stop drains the queue and waits for in-flight workers.
func (r *Runner) Stop() {
	if r == nil || !r.stopped.CompareAndSwap(false, true) {
		return
	}
	close(r.queue)
	r.wg.Wait()
}

func (r *Runner) run() {
	defer r.wg.Done()
	for p := range r.queue {
		r.score(p)
	}
}

// rollSample returns true when the request is selected for scoring,
// gated by the policy's sample_pct (1..100). 0 disables online eval.
func (r *Runner) rollSample(pct int) bool {
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	r.randMu.Lock()
	defer r.randMu.Unlock()
	return r.rand.Intn(100) < pct
}

func (r *Runner) score(p *Pending) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pol, err := r.store.GetRoutingPolicy(ctx, p.RequestedAlias, p.OrgID)
	if err != nil {
		r.logger.Debug("qualityeval: policy lookup", zap.Error(err))
	}
	if pol == nil {
		// Fall back to global default.
		pol, _ = r.store.GetRoutingPolicy(ctx, p.RequestedAlias, nil)
	}
	samplePct := 0
	judgeAlias := r.defaultJudge
	if pol != nil {
		samplePct = pol.SamplePct
		if pol.JudgeAlias != nil && *pol.JudgeAlias != "" {
			judgeAlias = *pol.JudgeAlias
		}
	}
	if !r.rollSample(samplePct) {
		return
	}
	if judgeAlias == "" {
		return
	}

	score, pass, detail, err := r.runJudge(ctx, judgeAlias, p)
	if err != nil {
		r.logger.Debug("qualityeval: judge run failed", zap.Error(err))
		return
	}

	// eval_scores requires a recording row; skip when we don't have one.
	if p.RecordingID != nil {
		ev := &models.EvalScore{
			RecordingID: *p.RecordingID,
			Scorer:      "online_llm_judge",
			Score:       &score,
			Pass:        &pass,
			Detail:      detail,
		}
		if err := r.store.InsertEvalScore(ctx, ev); err != nil {
			r.logger.Debug("qualityeval: insert eval_score", zap.Error(err))
		}
	}

	// feedback_events feeds the smartroute trainer. We emit two
	// metrics: a continuous quality_score (0..1) and a binary
	// quality_pass (>=threshold). The trainer can choose which one
	// it wants as the supervised label.
	scoreFloat := score
	passBool := pass
	if err := r.store.InsertFeedback(ctx, &models.FeedbackEvent{
		TraceID:    p.TraceID,
		Metric:     "quality_score",
		ValueFloat: &scoreFloat,
		ModelAlias: p.ServedAlias,
		OrgID:      p.OrgID,
		APIKeyID:   p.APIKeyID,
	}); err != nil {
		r.logger.Debug("qualityeval: insert feedback (score)", zap.Error(err))
	}
	if err := r.store.InsertFeedback(ctx, &models.FeedbackEvent{
		TraceID:    p.TraceID,
		Metric:     "quality_pass",
		ValueBool:  &passBool,
		ModelAlias: p.ServedAlias,
		OrgID:      p.OrgID,
		APIKeyID:   p.APIKeyID,
	}); err != nil {
		r.logger.Debug("qualityeval: insert feedback (pass)", zap.Error(err))
	}
}

// runJudge wraps replay.LLMJudge to score response-vs-itself (a
// degraded-but-real signal: the judge effectively rates absolute
// answer quality given the question, since the original is the same
// as the replay). When a customer wires up a true counterfactual
// recording (e.g. via shadow_learn), we'll pass the baseline response
// in here instead.
func (r *Runner) runJudge(ctx context.Context, judgeAlias string, p *Pending) (float64, bool, []byte, error) {
	if p.Response == nil {
		return 0, false, nil, errors.New("no response to judge")
	}
	scorer := &replay.LLMJudge{
		Model:  judgeAlias,
		Invoke: r.invoke,
	}
	// Score against itself when no counterfactual is available.
	other := p.Response
	s, err := scorer.Score(ctx, p.Response, other)
	if err != nil {
		return 0, false, nil, err
	}
	var detail []byte
	if len(s.Detail) > 0 {
		detail = []byte(s.Detail)
	} else {
		detail, _ = json.Marshal(map[string]any{"requested_alias": p.RequestedAlias, "served_alias": p.ServedAlias})
	}
	return s.Value, s.Pass, detail, nil
}
