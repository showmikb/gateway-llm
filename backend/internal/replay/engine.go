package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/ir"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/types"
	"go.uber.org/zap"
)

// ChatExecutor runs a chat request against a model alias and returns the
// resulting ir.ChatResponse. Supplied by the server so the replay engine
// doesn't depend on the HTTP layer.
type ChatExecutor func(ctx context.Context, alias string, req *ir.ChatRequest) (*ir.ChatResponse, error)

// Engine executes a replay run: for each recording in the configured
// filter, it re-runs the original request against the target model alias
// and scores the result against the original response.
//
// Runs execute with bounded concurrency so a 10k-recording replay doesn't
// hammer the upstream model. Progress is updated on the run row as we go.
type Engine struct {
	db       *db.DB
	store    BlobStore
	executor ChatExecutor
	logger   *zap.Logger
	parallel int
}

func NewEngine(database *db.DB, store BlobStore, executor ChatExecutor, logger *zap.Logger) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Engine{
		db:       database,
		store:    store,
		executor: executor,
		logger:   logger,
		parallel: 4,
	}
}

// RunOptions specify what a replay run touches.
type RunOptions struct {
	Name        string
	OrgID       *uuid.UUID
	CreatedBy   *uuid.UUID
	TargetAlias string // alias to replay against; empty = replay against original alias
	Scorer      string
	ScorerCfg   json.RawMessage
	Filter      db.ListRecordingsFilter
}

// Start creates a replay_runs row and kicks off workers in the background.
// Returns the new run ID immediately so the UI can poll progress.
func (e *Engine) Start(ctx context.Context, opts RunOptions) (*models.ReplayRun, error) {
	if opts.Name == "" {
		opts.Name = "replay-" + time.Now().UTC().Format(time.RFC3339)
	}
	// Enumerate recordings up-front so progress counters are accurate.
	recs, err := e.db.ListRecordings(ctx, opts.Filter)
	if err != nil {
		return nil, fmt.Errorf("list recordings: %w", err)
	}
	run := &models.ReplayRun{
		ID:           uuid.New(),
		Name:         opts.Name,
		TargetAlias:  opts.TargetAlias,
		Scorer:       opts.Scorer,
		ScorerConfig: opts.ScorerCfg,
		CreatedBy:    opts.CreatedBy,
		OrgID:        opts.OrgID,
		Status:       "pending",
		Total:        len(recs),
	}
	if err := e.db.CreateReplayRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	go e.execute(run, recs, opts)
	return run, nil
}

// execute runs the replay worker pool. It updates run status at start,
// every 25 results, and at end.
func (e *Engine) execute(run *models.ReplayRun, recs []models.Recording, opts RunOptions) {
	bg, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	now := time.Now().UTC()
	run.Status = "running"
	run.StartedAt = &now
	_ = e.db.UpdateReplayRun(bg, run)

	scorer, err := NewScorerByName(run.Scorer, run.ScorerConfig)
	if err != nil {
		e.logger.Warn("scorer init", zap.Error(err))
		scorer = &CosineScorer{Threshold: 0.75}
	}

	sem := make(chan struct{}, e.parallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var totalScore float64
	completed := 0
	failed := 0

	for i := range recs {
		rec := recs[i]
		sem <- struct{}{}
		wg.Add(1)
		go func(rec models.Recording) {
			defer func() { <-sem; wg.Done() }()
			result := e.replayOne(bg, run, scorer, &rec, opts.TargetAlias)

			mu.Lock()
			defer mu.Unlock()
			if err := e.db.InsertReplayResult(bg, result); err != nil {
				e.logger.Warn("insert replay result", zap.Error(err))
			}
			if result.Status == "ok" && result.Score != nil {
				totalScore += *result.Score
				completed++
			} else {
				failed++
			}
			if (completed+failed)%25 == 0 {
				run.Completed = completed
				run.Failed = failed
				if completed > 0 {
					avg := totalScore / float64(completed)
					run.AvgScore = &avg
				}
				_ = e.db.UpdateReplayRun(bg, run)
			}
		}(rec)
	}
	wg.Wait()

	finished := time.Now().UTC()
	run.Status = "completed"
	run.Completed = completed
	run.Failed = failed
	if completed > 0 {
		avg := totalScore / float64(completed)
		run.AvgScore = &avg
	}
	run.FinishedAt = &finished
	_ = e.db.UpdateReplayRun(bg, run)
}

// replayOne re-runs a single recording and scores the result. Failures
// don't halt the run; they're recorded as individual result rows with
// error text so the UI can surface them.
func (e *Engine) replayOne(ctx context.Context, run *models.ReplayRun, scorer Scorer, rec *models.Recording, targetAlias string) *models.ReplayResult {
	result := &models.ReplayResult{
		ID:          uuid.New(),
		RunID:       run.ID,
		RecordingID: &rec.ID,
		TargetAlias: targetAlias,
		Status:      "ok",
	}
	if targetAlias == "" {
		result.TargetAlias = rec.ModelAlias
		targetAlias = rec.ModelAlias
	}

	// Load original request/response from the blob store.
	reqBytes, err := e.store.Get(ctx, rec.RequestBlob)
	if err != nil {
		result.Status = "error"
		result.Error = "fetch request blob: " + err.Error()
		return result
	}
	var reqIR ir.ChatRequest
	if err := json.Unmarshal(reqBytes, &reqIR); err != nil {
		result.Status = "error"
		result.Error = "parse request: " + err.Error()
		return result
	}
	reqIR.Alias = targetAlias
	reqIR.Stream = false // force non-streaming for deterministic replay

	var origResp *ir.ChatResponse
	if rec.ResponseBlob != "" {
		if respBytes, err := e.store.Get(ctx, rec.ResponseBlob); err == nil {
			origResp = &ir.ChatResponse{}
			_ = json.Unmarshal(respBytes, origResp)
		}
	}

	// Execute the replay.
	start := time.Now()
	replayResp, err := e.executor(ctx, targetAlias, &reqIR)
	result.LatencyMS = int(time.Since(start).Milliseconds())
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}

	// Persist the replay response body as a new blob so the UI can diff.
	respBytes, _ := json.Marshal(replayResp)
	if url, err := e.store.Put(ctx, "replay", respBytes); err == nil {
		result.ResponseBlob = url
		result.ResponseHash = sha256Hex(respBytes)
	}

	if replayResp.Usage != nil {
		// Cost tracking for the replay itself is wired via the executor's
		// normal spend path; the UI reads it from the result blob too.
	}

	score, err := scorer.Score(ctx, origResp, replayResp)
	if err != nil {
		result.Status = "error"
		result.Error = "score: " + err.Error()
		return result
	}
	v := score.Value
	result.Score = &v
	result.EvalDetail = score.Detail
	return result
}

// --- Online eval (synchronous) -----------------------------------------

// EvalOne runs a single scorer over a recording and persists the score. This
// is how online eval attaches scores to production requests in near real time.
func EvalOne(ctx context.Context, database *db.DB, store BlobStore, recID uuid.UUID, scorer Scorer) (*models.EvalScore, error) {
	rec, err := database.GetRecording(ctx, recID)
	if err != nil {
		return nil, err
	}
	if rec.ResponseBlob == "" {
		return nil, fmt.Errorf("recording has no response blob")
	}
	respBytes, err := store.Get(ctx, rec.ResponseBlob)
	if err != nil {
		return nil, err
	}
	var resp ir.ChatResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	score, err := scorer.Score(ctx, nil, &resp)
	if err != nil {
		return nil, err
	}
	ev := &models.EvalScore{
		ID:          uuid.New(),
		RecordingID: recID,
		Scorer:      scorer.Name(),
		Score:       &score.Value,
		Pass:        &score.Pass,
		Detail:      score.Detail,
	}
	if err := database.InsertEvalScore(ctx, ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// --- helper to unused compile-time references --------------------------

var _ = types.ChatCompletionRequest{} // keep types import (used when engine is wired to HTTP)
