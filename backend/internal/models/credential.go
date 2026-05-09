package models

import (
	"time"

	"github.com/google/uuid"
)

type ProviderCredential struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Provider   string     `json:"provider"`
	APIKeyEnc  []byte     `json:"-"`
	APIKeyMask string     `json:"api_key_masked,omitempty"`
	APIBase    string     `json:"api_base,omitempty"`
	OrgID      *uuid.UUID `json:"org_id,omitempty"`
	IsActive   bool       `json:"is_active"`
	CreatedBy  *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
