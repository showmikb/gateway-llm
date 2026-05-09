package models

import (
	"time"

	"github.com/google/uuid"
)

type Organization struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	MaxBudget  *float64  `json:"max_budget,omitempty"`
	TotalSpend float64   `json:"total_spend"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
