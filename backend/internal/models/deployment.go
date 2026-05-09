package models

import (
	"time"

	"github.com/google/uuid"
)

type Deployment struct {
	ID              uuid.UUID  `json:"id"`
	ModelAlias      string     `json:"model_alias"`
	Provider        string     `json:"provider"`
	ProviderModel   string     `json:"provider_model"`
	APIKeyEnv       string     `json:"api_key_env,omitempty"`
	CredentialID    *uuid.UUID `json:"credential_id,omitempty"`
	CredentialName  string     `json:"credential_name,omitempty"`
	OrgID           *uuid.UUID `json:"org_id,omitempty"`
	APIBase         string     `json:"api_base,omitempty"`
	Capabilities    []string   `json:"capabilities,omitempty"`
	Priority        int        `json:"priority"`
	Weight          int        `json:"weight"`
	// VariantID optionally tags a deployment as a named variant (e.g.
	// "control", "gpt4o-cot") for A/B/experimentation analytics.
	VariantID       string     `json:"variant_id,omitempty"`
	RoutingStrategy string     `json:"routing_strategy"`
	IsActive        bool       `json:"is_active"`
	CreatedAt       time.Time  `json:"created_at"`
}
