-- +migrate Up

CREATE TABLE IF NOT EXISTS provider_credentials (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    provider    TEXT NOT NULL,
    api_key_enc BYTEA NOT NULL,
    api_base    TEXT,
    is_active   BOOLEAN DEFAULT TRUE,
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_provider_creds_name ON provider_credentials(name);

ALTER TABLE deployments ADD COLUMN IF NOT EXISTS credential_id UUID REFERENCES provider_credentials(id);

-- +migrate Down

ALTER TABLE deployments DROP COLUMN IF EXISTS credential_id;
DROP TABLE IF EXISTS provider_credentials;
