package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Recording is the per-request ground-truth row we persist every time a
// caller opts into recording. The full request/response payloads live in
// RequestBlob / ResponseBlob (an s3://… or file://… URL); the row contains
// only indexable metadata.
type Recording struct {
	ID               uuid.UUID       `json:"id"`
	TraceID          string          `json:"trace_id,omitempty"`
	APIKeyID         *uuid.UUID      `json:"api_key_id,omitempty"`
	UserID           *uuid.UUID      `json:"user_id,omitempty"`
	TeamID           *uuid.UUID      `json:"team_id,omitempty"`
	OrgID            *uuid.UUID      `json:"org_id,omitempty"`
	ModelAlias       string          `json:"model_alias"`
	Provider         string          `json:"provider"`
	ProviderModel    string          `json:"provider_model"`
	Endpoint         string          `json:"endpoint"`
	Ingress          string          `json:"ingress,omitempty"`
	StatusCode       int             `json:"status_code"`
	PromptTokens     int             `json:"prompt_tokens"`
	CompletionTokens int             `json:"completion_tokens"`
	TotalTokens      int             `json:"total_tokens"`
	CostUSD          float64         `json:"cost_usd"`
	LatencyMS        int             `json:"latency_ms"`
	RouterReason     string          `json:"router_reason,omitempty"`
	EvalScore        *float64        `json:"eval_score,omitempty"`
	Tags             []string        `json:"tags,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	RequestBlob      string          `json:"request_blob,omitempty"`
	ResponseBlob     string          `json:"response_blob,omitempty"`
	RequestHash      string          `json:"request_hash"`
	ResponseHash     string          `json:"response_hash,omitempty"`
	Redacted         bool            `json:"redacted"`
	CreatedAt        time.Time       `json:"created_at"`
}

// ReplayRun groups a batch of replays ("run every recording from last week
// against claude-sonnet-4-5 and score them").
type ReplayRun struct {
	ID           uuid.UUID       `json:"id"`
	Name         string          `json:"name"`
	TargetAlias  string          `json:"target_alias,omitempty"`
	Scorer       string          `json:"scorer,omitempty"`
	ScorerConfig json.RawMessage `json:"scorer_config,omitempty"`
	CreatedBy    *uuid.UUID      `json:"created_by,omitempty"`
	OrgID        *uuid.UUID      `json:"org_id,omitempty"`
	Status       string          `json:"status"`
	Total        int             `json:"total"`
	Completed    int             `json:"completed"`
	Failed       int             `json:"failed"`
	AvgScore     *float64        `json:"avg_score,omitempty"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type ReplayResult struct {
	ID           uuid.UUID       `json:"id"`
	RunID        uuid.UUID       `json:"run_id"`
	RecordingID  *uuid.UUID      `json:"recording_id,omitempty"`
	TargetAlias  string          `json:"target_alias,omitempty"`
	Status       string          `json:"status"`
	Score        *float64        `json:"score,omitempty"`
	CostUSD      float64         `json:"cost_usd"`
	LatencyMS    int             `json:"latency_ms"`
	ResponseBlob string          `json:"response_blob,omitempty"`
	ResponseHash string          `json:"response_hash,omitempty"`
	EvalDetail   json.RawMessage `json:"eval_detail,omitempty"`
	Error        string          `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type EvalScore struct {
	ID          uuid.UUID       `json:"id"`
	RecordingID uuid.UUID       `json:"recording_id"`
	Scorer      string          `json:"scorer"`
	Score       *float64        `json:"score,omitempty"`
	Pass        *bool           `json:"pass,omitempty"`
	Detail      json.RawMessage `json:"detail,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}
