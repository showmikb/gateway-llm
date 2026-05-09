package models

import (
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	OrgID      *uuid.UUID `json:"org_id,omitempty"`
	Models     []string   `json:"models,omitempty"`
	RPMLimit   *int       `json:"rpm_limit,omitempty"`
	TPMLimit   *int       `json:"tpm_limit,omitempty"`
	MaxBudget  *float64   `json:"max_budget,omitempty"`
	TotalSpend float64    `json:"total_spend"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
