-- +migrate Up

CREATE TABLE IF NOT EXISTS organizations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    slug        TEXT NOT NULL UNIQUE,
    max_budget  NUMERIC(12,4),
    total_spend NUMERIC(12,4) DEFAULT 0,
    is_active   BOOLEAN DEFAULT TRUE,
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);

ALTER TABLE teams ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);
ALTER TABLE users ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);
ALTER TABLE provider_credentials ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);

CREATE TABLE IF NOT EXISTS gateway_settings (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key        TEXT NOT NULL UNIQUE,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now()
);

-- +migrate Down

DROP TABLE IF EXISTS gateway_settings;
ALTER TABLE provider_credentials DROP COLUMN IF EXISTS org_id;
ALTER TABLE users DROP COLUMN IF EXISTS org_id;
ALTER TABLE teams DROP COLUMN IF EXISTS org_id;
DROP TABLE IF EXISTS organizations;
