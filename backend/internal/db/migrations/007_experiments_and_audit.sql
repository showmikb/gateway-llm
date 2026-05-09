-- +migrate Up

-- Variant tag for A/B testing. Purely informational; routing decisions
-- are driven by d.weight + d.routing_strategy, but this lets
-- observability/feedback tools segment traffic without reverse-engineering
-- weights.
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS variant_id TEXT NOT NULL DEFAULT '';

-- Append-only audit log for SOC2 / GDPR / HIPAA compliance. Insert only;
-- never UPDATE or DELETE.
CREATE TABLE IF NOT EXISTS audit_logs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_type  TEXT NOT NULL,
    actor_id    TEXT,
    action      TEXT NOT NULL,
    resource    TEXT,
    org_id      UUID,
    team_id     UUID,
    api_key_id  UUID,
    ip          INET,
    user_agent  TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_occurred_at ON audit_logs(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_org ON audit_logs(org_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);

-- Quality feedback attached to a prior inference by trace id. Used to
-- drive the SmartRoute learning loop and the /v1/metrics endpoint.
CREATE TABLE IF NOT EXISTS feedback_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    trace_id     TEXT NOT NULL,
    metric       TEXT NOT NULL,
    value_float  DOUBLE PRECISION,
    value_bool   BOOLEAN,
    value_text   TEXT,
    model_alias  TEXT,
    variant_id   TEXT,
    api_key_id   UUID,
    org_id       UUID
);
CREATE INDEX IF NOT EXISTS idx_feedback_trace ON feedback_events(trace_id);
CREATE INDEX IF NOT EXISTS idx_feedback_model_metric ON feedback_events(model_alias, metric, created_at DESC);

-- +migrate Down

DROP TABLE IF EXISTS feedback_events;
DROP TABLE IF EXISTS audit_logs;
ALTER TABLE deployments DROP COLUMN IF EXISTS variant_id;
