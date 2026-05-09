package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/models"
)

// InsertRecording persists a recording row. The blob URLs must already be
// populated (payload upload happens *before* the DB insert so a failed
// upload doesn't leave a dangling reference in the metadata table).
func (db *DB) InsertRecording(ctx context.Context, r *models.Recording) error {
	var metadata []byte
	if len(r.Metadata) > 0 {
		metadata = r.Metadata
	}
	_, err := db.pool.Exec(ctx, `
		INSERT INTO recordings (
			id, trace_id, api_key_id, user_id, team_id, org_id,
			model_alias, provider, provider_model, endpoint, ingress,
			status_code, prompt_tokens, completion_tokens, total_tokens,
			cost_usd, latency_ms, router_reason, eval_score, tags,
			metadata, request_blob, response_blob, request_hash,
			response_hash, redacted, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
		         $16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)`,
		r.ID, nullIfEmpty(r.TraceID), r.APIKeyID, r.UserID, r.TeamID, r.OrgID,
		r.ModelAlias, r.Provider, r.ProviderModel, r.Endpoint, nullIfEmpty(r.Ingress),
		r.StatusCode, r.PromptTokens, r.CompletionTokens, r.TotalTokens,
		r.CostUSD, r.LatencyMS, nullIfEmpty(r.RouterReason), r.EvalScore, r.Tags,
		metadata, nullIfEmpty(r.RequestBlob), nullIfEmpty(r.ResponseBlob),
		r.RequestHash, nullIfEmpty(r.ResponseHash), r.Redacted, r.CreatedAt)
	return err
}

// GetRecording returns a single recording with its blob URLs intact.
func (db *DB) GetRecording(ctx context.Context, id uuid.UUID) (*models.Recording, error) {
	r := &models.Recording{}
	var metaRaw []byte
	err := db.pool.QueryRow(ctx, `
		SELECT id, trace_id, api_key_id, user_id, team_id, org_id,
		       model_alias, provider, provider_model, endpoint, ingress,
		       status_code, prompt_tokens, completion_tokens, total_tokens,
		       cost_usd, latency_ms, router_reason, eval_score, tags,
		       metadata, request_blob, response_blob, request_hash,
		       response_hash, redacted, created_at
		FROM recordings WHERE id = $1`, id).Scan(
		&r.ID, &r.TraceID, &r.APIKeyID, &r.UserID, &r.TeamID, &r.OrgID,
		&r.ModelAlias, &r.Provider, &r.ProviderModel, &r.Endpoint, &r.Ingress,
		&r.StatusCode, &r.PromptTokens, &r.CompletionTokens, &r.TotalTokens,
		&r.CostUSD, &r.LatencyMS, &r.RouterReason, &r.EvalScore, &r.Tags,
		&metaRaw, &r.RequestBlob, &r.ResponseBlob, &r.RequestHash,
		&r.ResponseHash, &r.Redacted, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(metaRaw) > 0 {
		r.Metadata = json.RawMessage(metaRaw)
	}
	return r, nil
}

// ListRecordingsFilter is the filter shape for ListRecordings. Any zero-valued
// field means "no filter on that dimension".
type ListRecordingsFilter struct {
	OrgID      *uuid.UUID
	APIKeyID   *uuid.UUID
	ModelAlias string
	Tag        string
	TraceID    string
	Limit      int
	Offset     int
}

func (db *DB) ListRecordings(ctx context.Context, f ListRecordingsFilter) ([]models.Recording, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	args := []any{}
	where := "WHERE 1=1"
	if f.OrgID != nil {
		args = append(args, *f.OrgID)
		where += placeholder(" AND org_id = $", len(args))
	}
	if f.APIKeyID != nil {
		args = append(args, *f.APIKeyID)
		where += placeholder(" AND api_key_id = $", len(args))
	}
	if f.ModelAlias != "" {
		args = append(args, f.ModelAlias)
		where += placeholder(" AND model_alias = $", len(args))
	}
	if f.Tag != "" {
		args = append(args, f.Tag)
		where += placeholder(" AND $", len(args)) + " = ANY(tags)"
	}
	if f.TraceID != "" {
		args = append(args, f.TraceID)
		where += placeholder(" AND trace_id = $", len(args))
	}
	args = append(args, f.Limit, f.Offset)
	limitIdx := len(args) - 1
	q := `
		SELECT id, trace_id, api_key_id, user_id, team_id, org_id,
		       model_alias, provider, provider_model, endpoint, ingress,
		       status_code, prompt_tokens, completion_tokens, total_tokens,
		       cost_usd, latency_ms, router_reason, eval_score, tags,
		       metadata, request_blob, response_blob, request_hash,
		       response_hash, redacted, created_at
		FROM recordings ` + where + `
		ORDER BY created_at DESC LIMIT $` + itoa(limitIdx) + ` OFFSET $` + itoa(limitIdx+1)
	rows, err := db.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Recording
	for rows.Next() {
		var r models.Recording
		var metaRaw []byte
		if err := rows.Scan(
			&r.ID, &r.TraceID, &r.APIKeyID, &r.UserID, &r.TeamID, &r.OrgID,
			&r.ModelAlias, &r.Provider, &r.ProviderModel, &r.Endpoint, &r.Ingress,
			&r.StatusCode, &r.PromptTokens, &r.CompletionTokens, &r.TotalTokens,
			&r.CostUSD, &r.LatencyMS, &r.RouterReason, &r.EvalScore, &r.Tags,
			&metaRaw, &r.RequestBlob, &r.ResponseBlob, &r.RequestHash,
			&r.ResponseHash, &r.Redacted, &r.CreatedAt); err != nil {
			return nil, err
		}
		if len(metaRaw) > 0 {
			r.Metadata = json.RawMessage(metaRaw)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Replay runs + results ------------------------------------------------

func (db *DB) CreateReplayRun(ctx context.Context, r *models.ReplayRun) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	_, err := db.pool.Exec(ctx, `
		INSERT INTO replay_runs (id, name, target_alias, scorer, scorer_config,
		         created_by, org_id, status, total, completed, failed, avg_score,
		         started_at, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		r.ID, r.Name, nullIfEmpty(r.TargetAlias), nullIfEmpty(r.Scorer),
		rawOrNil(r.ScorerConfig), r.CreatedBy, r.OrgID, r.Status,
		r.Total, r.Completed, r.Failed, r.AvgScore, r.StartedAt, r.FinishedAt)
	return err
}

func (db *DB) UpdateReplayRun(ctx context.Context, r *models.ReplayRun) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE replay_runs SET status=$2, completed=$3, failed=$4, avg_score=$5,
		       started_at=$6, finished_at=$7 WHERE id=$1`,
		r.ID, r.Status, r.Completed, r.Failed, r.AvgScore, r.StartedAt, r.FinishedAt)
	return err
}

func (db *DB) GetReplayRun(ctx context.Context, id uuid.UUID) (*models.ReplayRun, error) {
	r := &models.ReplayRun{}
	var scorerCfg []byte
	err := db.pool.QueryRow(ctx, `
		SELECT id, name, target_alias, scorer, scorer_config, created_by, org_id,
		       status, total, completed, failed, avg_score, started_at, finished_at, created_at
		FROM replay_runs WHERE id = $1`, id).Scan(
		&r.ID, &r.Name, &r.TargetAlias, &r.Scorer, &scorerCfg, &r.CreatedBy, &r.OrgID,
		&r.Status, &r.Total, &r.Completed, &r.Failed, &r.AvgScore,
		&r.StartedAt, &r.FinishedAt, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(scorerCfg) > 0 {
		r.ScorerConfig = json.RawMessage(scorerCfg)
	}
	return r, nil
}

func (db *DB) InsertReplayResult(ctx context.Context, r *models.ReplayResult) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	_, err := db.pool.Exec(ctx, `
		INSERT INTO replay_results (id, run_id, recording_id, target_alias, status,
		         score, cost_usd, latency_ms, response_blob, response_hash,
		         eval_detail, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		r.ID, r.RunID, r.RecordingID, nullIfEmpty(r.TargetAlias), r.Status,
		r.Score, r.CostUSD, r.LatencyMS, nullIfEmpty(r.ResponseBlob),
		nullIfEmpty(r.ResponseHash), rawOrNil(r.EvalDetail), nullIfEmpty(r.Error))
	return err
}

func (db *DB) ListReplayResults(ctx context.Context, runID uuid.UUID, limit, offset int) ([]models.ReplayResult, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.pool.Query(ctx, `
		SELECT id, run_id, recording_id, target_alias, status, score, cost_usd,
		       latency_ms, response_blob, response_hash, eval_detail, error, created_at
		FROM replay_results WHERE run_id = $1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, runID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ReplayResult
	for rows.Next() {
		var r models.ReplayResult
		var detail []byte
		if err := rows.Scan(&r.ID, &r.RunID, &r.RecordingID, &r.TargetAlias, &r.Status,
			&r.Score, &r.CostUSD, &r.LatencyMS, &r.ResponseBlob, &r.ResponseHash,
			&detail, &r.Error, &r.CreatedAt); err != nil {
			return nil, err
		}
		if len(detail) > 0 {
			r.EvalDetail = json.RawMessage(detail)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Eval scores ----------------------------------------------------------

func (db *DB) InsertEvalScore(ctx context.Context, s *models.EvalScore) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	_, err := db.pool.Exec(ctx, `
		INSERT INTO eval_scores (id, recording_id, scorer, score, pass, detail)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		s.ID, s.RecordingID, s.Scorer, s.Score, s.Pass, rawOrNil(s.Detail))
	return err
}

func (db *DB) ListEvalScores(ctx context.Context, recordingID uuid.UUID) ([]models.EvalScore, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, recording_id, scorer, score, pass, detail, created_at
		FROM eval_scores WHERE recording_id = $1 ORDER BY created_at DESC`, recordingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.EvalScore
	for rows.Next() {
		var s models.EvalScore
		var detail []byte
		if err := rows.Scan(&s.ID, &s.RecordingID, &s.Scorer, &s.Score, &s.Pass, &detail, &s.CreatedAt); err != nil {
			return nil, err
		}
		if len(detail) > 0 {
			s.Detail = json.RawMessage(detail)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// --- small helpers --------------------------------------------------------

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func rawOrNil(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return []byte(r)
}

func placeholder(prefix string, n int) string { return prefix + itoa(n) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
