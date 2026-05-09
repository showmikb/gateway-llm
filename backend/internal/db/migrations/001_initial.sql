-- +migrate Up

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE teams (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    models        TEXT[],
    rpm_limit     INT,
    tpm_limit     INT,
    max_budget    NUMERIC(12,4),
    total_spend   NUMERIC(12,4) DEFAULT 0,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT UNIQUE,
    role          TEXT DEFAULT 'user',
    team_id       UUID REFERENCES teams(id),
    created_at    TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE api_keys (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash    TEXT NOT NULL UNIQUE,
    name          TEXT,
    team_id       UUID REFERENCES teams(id),
    user_id       UUID REFERENCES users(id),
    models        TEXT[],
    rpm_limit     INT,
    tpm_limit     INT,
    max_budget    NUMERIC(12,4),
    total_spend   NUMERIC(12,4) DEFAULT 0,
    is_active     BOOLEAN DEFAULT TRUE,
    expires_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_api_keys_token_hash ON api_keys(token_hash);

CREATE TABLE deployments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_alias     TEXT NOT NULL,
    provider        TEXT NOT NULL,
    provider_model  TEXT NOT NULL,
    api_key_env     TEXT NOT NULL,
    api_base        TEXT,
    capabilities    TEXT[] DEFAULT '{}',
    priority        INT DEFAULT 0,
    is_active       BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_deployments_alias ON deployments(model_alias);

CREATE TABLE spend_logs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_key_id        UUID REFERENCES api_keys(id),
    deployment_id     UUID REFERENCES deployments(id),
    model_alias       TEXT NOT NULL,
    provider          TEXT NOT NULL,
    endpoint          TEXT NOT NULL,
    prompt_tokens     INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    total_tokens      INT NOT NULL DEFAULT 0,
    cost_usd          NUMERIC(12,6) DEFAULT 0,
    latency_ms        INT,
    status_code       INT,
    created_at        TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_spend_logs_key ON spend_logs(api_key_id, created_at);
CREATE INDEX idx_spend_logs_daily ON spend_logs(created_at);
CREATE INDEX idx_spend_logs_endpoint ON spend_logs(endpoint, created_at);

CREATE TABLE custom_pricing (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider                  TEXT NOT NULL,
    model                     TEXT NOT NULL,
    deployment_id             UUID REFERENCES deployments(id),
    input_cost_per_token      NUMERIC(20,12),
    output_cost_per_token     NUMERIC(20,12),
    cache_read_cost_per_token NUMERIC(20,12),
    input_cost_per_image      NUMERIC(12,6),
    input_cost_per_character  NUMERIC(20,12),
    input_cost_per_second     NUMERIC(12,6),
    size_pricing              JSONB,
    created_at                TIMESTAMPTZ DEFAULT now(),
    updated_at                TIMESTAMPTZ DEFAULT now(),
    UNIQUE(provider, model, deployment_id)
);
CREATE INDEX idx_custom_pricing_lookup ON custom_pricing(provider, model);

CREATE TABLE response_objects (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    response_id     TEXT NOT NULL UNIQUE,
    api_key_id      UUID REFERENCES api_keys(id),
    provider        TEXT NOT NULL,
    model_alias     TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'completed',
    request_body    JSONB,
    response_body   JSONB,
    created_at      TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_response_objects_rid ON response_objects(response_id);

CREATE TABLE daily_spend (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_key_id      UUID REFERENCES api_keys(id),
    team_id         UUID REFERENCES teams(id),
    date            DATE NOT NULL,
    total_tokens    BIGINT DEFAULT 0,
    total_cost_usd  NUMERIC(12,4) DEFAULT 0,
    request_count   INT DEFAULT 0,
    UNIQUE(api_key_id, team_id, date)
);

-- +migrate Down
DROP TABLE IF EXISTS daily_spend;
DROP TABLE IF EXISTS response_objects;
DROP TABLE IF EXISTS custom_pricing;
DROP TABLE IF EXISTS spend_logs;
DROP TABLE IF EXISTS deployments;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS teams;
