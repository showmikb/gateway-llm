package models

import (
	"time"

	"github.com/google/uuid"
)

type APIKey struct {
	ID        uuid.UUID  `json:"id"`
	TokenHash string     `json:"-"`
	Name      string     `json:"name"`
	TeamID    *uuid.UUID `json:"team_id,omitempty"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	Models    []string   `json:"models,omitempty"`
	RPMLimit  *int       `json:"rpm_limit,omitempty"`
	TPMLimit  *int       `json:"tpm_limit,omitempty"`
	MaxBudget *float64   `json:"max_budget,omitempty"`
	TotalSpend float64   `json:"total_spend"`
	IsActive  bool       `json:"is_active"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
