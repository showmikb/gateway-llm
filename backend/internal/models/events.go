package models

import (
	"time"

	"github.com/google/uuid"
)

// FeedbackEvent captures a quality signal emitted by a client for a
// previously-served inference. Each row is immutable; downstream metrics
// and SmartRoute calibration read from this table.
type FeedbackEvent struct {
	ID         uuid.UUID  `json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	TraceID    string     `json:"trace_id"`
	Metric     string     `json:"metric"`
	ValueFloat *float64   `json:"value_float,omitempty"`
	ValueBool  *bool      `json:"value_bool,omitempty"`
	ValueText  string     `json:"value_text,omitempty"`
	ModelAlias string     `json:"model_alias,omitempty"`
	VariantID  string     `json:"variant_id,omitempty"`
	APIKeyID   *uuid.UUID `json:"api_key_id,omitempty"`
	OrgID      *uuid.UUID `json:"org_id,omitempty"`
}

// AuditEvent is an append-only record of a security-relevant action.
// Rows are never updated or deleted; downstream compliance reports
// aggregate from this table.
type AuditEvent struct {
	ID         uuid.UUID      `json:"id"`
	OccurredAt time.Time      `json:"occurred_at"`
	ActorType  string         `json:"actor_type"`
	ActorID    string         `json:"actor_id,omitempty"`
	Action     string         `json:"action"`
	Resource   string         `json:"resource,omitempty"`
	OrgID      *uuid.UUID     `json:"org_id,omitempty"`
	TeamID     *uuid.UUID     `json:"team_id,omitempty"`
	APIKeyID   *uuid.UUID     `json:"api_key_id,omitempty"`
	IP         string         `json:"ip,omitempty"`
	UserAgent  string         `json:"user_agent,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}
