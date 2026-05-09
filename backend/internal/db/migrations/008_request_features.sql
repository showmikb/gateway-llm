-- +migrate Up

-- Per-request feature snapshots written at inference time. SmartRoute's
-- Phase-2 ML trainer JOINs these on trace_id with feedback_events to
-- build a (features -> label) dataset without replaying raw prompts.
CREATE TABLE IF NOT EXISTS request_features (
    trace_id     TEXT PRIMARY KEY,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    model_alias  TEXT NOT NULL,
    org_id       UUID,
    api_key_id   UUID,
    features     JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_request_features_created ON request_features(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_features_alias ON request_features(model_alias, created_at DESC);

-- +migrate Down

DROP TABLE IF EXISTS request_features;
