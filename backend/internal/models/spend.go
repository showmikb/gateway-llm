package models

import (
	"time"

	"github.com/google/uuid"
)

type SpendLog struct {
	ID               uuid.UUID  `json:"id"`
	APIKeyID         *uuid.UUID `json:"api_key_id,omitempty"`
	DeploymentID     *uuid.UUID `json:"deployment_id,omitempty"`
	ModelAlias       string     `json:"model_alias"`
	Provider         string     `json:"provider"`
	Endpoint         string     `json:"endpoint"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	TotalTokens      int        `json:"total_tokens"`
	CostUSD          float64    `json:"cost_usd"`
	LatencyMS        int        `json:"latency_ms"`
	StatusCode       int        `json:"status_code"`
	TraceID          string     `json:"trace_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type DailySpend struct {
	ID           uuid.UUID  `json:"id"`
	APIKeyID     *uuid.UUID `json:"api_key_id,omitempty"`
	TeamID       *uuid.UUID `json:"team_id,omitempty"`
	Date         time.Time  `json:"date"`
	TotalTokens  int64      `json:"total_tokens"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	RequestCount int        `json:"request_count"`
}

type CustomPricing struct {
	ID                    uuid.UUID  `json:"id"`
	Provider              string     `json:"provider"`
	Model                 string     `json:"model"`
	DeploymentID          *uuid.UUID `json:"deployment_id,omitempty"`
	InputCostPerToken     *float64   `json:"input_cost_per_token,omitempty"`
	OutputCostPerToken    *float64   `json:"output_cost_per_token,omitempty"`
	CacheReadCostPerToken *float64   `json:"cache_read_cost_per_token,omitempty"`
	InputCostPerImage     *float64   `json:"input_cost_per_image,omitempty"`
	InputCostPerCharacter *float64   `json:"input_cost_per_character,omitempty"`
	InputCostPerSecond    *float64   `json:"input_cost_per_second,omitempty"`
	SizePricing           map[string]float64 `json:"size_pricing,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}
