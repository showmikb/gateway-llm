package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID  `json:"id"`
	Email        string     `json:"email"`
	Role         string     `json:"role"`
	TeamID       *uuid.UUID `json:"team_id,omitempty"`
	OrgID        *uuid.UUID `json:"org_id,omitempty"`
	PasswordHash string     `json:"-"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
