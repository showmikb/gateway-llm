-- +migrate Up

-- Pillar: Smart-Routing Moat.
-- Five new tables that turn Gateway-LLM's heuristic+ML smart router into a
-- billable, auditable savings product. The high-level shape:
--
--   operator_price_catalog  -- vendor list prices, signed by the gateway
--                              operator. The ONLY source of truth for the
--                              "would-have-cost" baseline used in savings
--                              math; org admins cannot touch it.
--   org_provider_discounts  -- per-tenant negotiated discount % (e.g.
--                              "we get 15% off Vertex AI"). Declared by
--                              org admin, optionally countersigned by the
--                              operator before it affects billing.
--   routing_policy          -- per-alias routing strategy + quality knobs.
--                              Five strategies: off, track_only,
--                              auto_retry, judge_then_decide, shadow_learn.
--   routing_savings         -- per-request, signed ledger entry capturing
--                              baseline_cost - actual_cost = savings_usd.
--                              Hash-chained per-org so the entire ledger
--                              is offline-verifiable from a single key.
--   daily_savings           -- rollups for the admin dashboard + invoicing.

-- ---- 1. operator_price_catalog -------------------------------------------
-- Replaces the embedded model_prices.json as billing-truth at runtime.
-- Older rows are kept (effective_to set) so receipts and savings rows
-- written months ago can still be re-priced exactly using the catalog
-- that was in force at the time. signature is Ed25519 over a canonical
-- representation (see internal/cost/catalog_canonical.go).
CREATE TABLE IF NOT EXISTS operator_price_catalog (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider                    TEXT NOT NULL,
    model                       TEXT NOT NULL,
    mode                        TEXT,
    input_cost_per_token        NUMERIC(20,12) NOT NULL DEFAULT 0,
    output_cost_per_token       NUMERIC(20,12) NOT NULL DEFAULT 0,
    cache_read_cost_per_token   NUMERIC(20,12),
    input_cost_per_image        NUMERIC(12,6),
    input_cost_per_character    NUMERIC(20,12),
    input_cost_per_second       NUMERIC(12,6),
    max_input_tokens            INT,
    max_output_tokens           INT,
    size_pricing                JSONB,
    source                      TEXT NOT NULL DEFAULT 'manual',  -- manual|sync|seed
    effective_from              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    effective_to                TIMESTAMPTZ,
    signed_at                   TIMESTAMPTZ,
    signature                   TEXT,                            -- base64 ed25519
    signed_by_key_id            TEXT,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_opc_lookup ON operator_price_catalog(provider, model, effective_from DESC);
CREATE INDEX IF NOT EXISTS idx_opc_active ON operator_price_catalog(provider, model)
    WHERE effective_to IS NULL;

-- ---- 2. org_provider_discounts -------------------------------------------
-- An org admin can DECLARE a discount; the operator (master key) must
-- countersign before it affects billing or savings math. Until then the
-- declaration is visible in the UI but ignored by cost.Engine.
-- org_id NULL is reserved for the operator's own tenant in self-hosted
-- single-tenant deploys.
CREATE TABLE IF NOT EXISTS org_provider_discounts (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id              UUID REFERENCES organizations(id) ON DELETE CASCADE,
    provider            TEXT NOT NULL,
    discount_pct        NUMERIC(5,4) NOT NULL,                   -- 0.15 = 15% off
    evidence_url        TEXT,
    evidence_note       TEXT,
    declared_by         UUID REFERENCES users(id) ON DELETE SET NULL,
    declared_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attested_by         UUID REFERENCES users(id) ON DELETE SET NULL,
    attested_at         TIMESTAMPTZ,
    operator_signature  TEXT,                                    -- base64 ed25519
    effective_from      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    effective_to        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_discount_range CHECK (discount_pct >= 0 AND discount_pct < 1)
);
CREATE INDEX IF NOT EXISTS idx_opd_lookup ON org_provider_discounts(org_id, provider, effective_from DESC);
-- Only one active (non-superseded) declaration per (org, provider) at a
-- time. Postgres treats NULLs as distinct so an org can have multiple
-- expired (effective_to set) rows.
CREATE UNIQUE INDEX IF NOT EXISTS uq_opd_active
    ON org_provider_discounts(COALESCE(org_id::text, ''), provider)
    WHERE effective_to IS NULL;

-- ---- 3. routing_policy ----------------------------------------------------
-- One row per (alias, org). org_id NULL = the global default that applies
-- when no per-org override exists. strategy controls how the chat handler
-- treats the smart-route Decision:
--
--   off                  -- bypass smartroute entirely
--   track_only           -- route to cheap if Decision says so, log only
--   auto_retry           -- route, post-eval; if quality score < threshold
--                          and request is non-streaming, retry baseline
--                          and serve baseline. The quality-first default.
--   judge_then_decide    -- route to cheap, run inline LLM judge, swap to
--                          baseline before returning if judge says weak.
--                          Adds ~50-200ms; only for non-streaming.
--   shadow_learn         -- always serve baseline; run cheap as shadow to
--                          collect quality data without customer risk.
--                          Used during cold start.
CREATE TABLE IF NOT EXISTS routing_policy (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_alias                     TEXT NOT NULL,
    org_id                          UUID REFERENCES organizations(id) ON DELETE CASCADE,
    strategy                        TEXT NOT NULL DEFAULT 'track_only',
    quality_threshold               DOUBLE PRECISION NOT NULL DEFAULT 0.7,
    baseline_provider_model         TEXT,                       -- e.g. "openai/gpt-4o"
    judge_alias                     TEXT,                       -- alias used for inline judge
    retry_when_streaming            BOOLEAN NOT NULL DEFAULT FALSE,
    sample_pct                      INT NOT NULL DEFAULT 5,     -- async judge sample %
    min_samples_before_routing      INT NOT NULL DEFAULT 0,     -- 0 = route immediately
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_strategy CHECK (strategy IN ('off','track_only','auto_retry','judge_then_decide','shadow_learn')),
    CONSTRAINT chk_threshold CHECK (quality_threshold >= 0 AND quality_threshold <= 1),
    CONSTRAINT chk_sample CHECK (sample_pct >= 0 AND sample_pct <= 100)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_routing_policy
    ON routing_policy(model_alias, COALESCE(org_id::text, ''));
CREATE INDEX IF NOT EXISTS idx_routing_policy_alias ON routing_policy(model_alias);

-- ---- 4. routing_savings ---------------------------------------------------
-- The core moat ledger. One row per request that smart-routing observed,
-- whether or not it overrode the alias. baseline_cost_usd is what the
-- request would have cost on the customer's stated baseline model at the
-- customer's negotiated rate; actual_cost_usd is what we actually billed
-- after routing. savings_usd = baseline - actual (can be negative when
-- the override produced more tokens; we don't lie about that).
--
-- The signature column makes the ledger auditable from a single Ed25519
-- public key without trusting Postgres. prev_hash chains rows per-org so
-- gaps or reorderings are detectable offline.
CREATE TABLE IF NOT EXISTS routing_savings (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recording_id                UUID REFERENCES recordings(id) ON DELETE SET NULL,
    trace_id                    TEXT,
    org_id                      UUID REFERENCES organizations(id) ON DELETE SET NULL,
    api_key_id                  UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    requested_alias             TEXT NOT NULL,
    served_alias                TEXT NOT NULL,
    served_provider             TEXT,
    served_model                TEXT,
    baseline_provider           TEXT,
    baseline_model              TEXT,
    baseline_input_tokens       INT NOT NULL DEFAULT 0,
    baseline_output_tokens      INT NOT NULL DEFAULT 0,
    served_input_tokens         INT NOT NULL DEFAULT 0,
    served_output_tokens        INT NOT NULL DEFAULT 0,
    baseline_cost_usd           NUMERIC(14,8) NOT NULL DEFAULT 0,
    actual_cost_usd             NUMERIC(14,8) NOT NULL DEFAULT 0,
    discount_pct_applied        NUMERIC(5,4) NOT NULL DEFAULT 0,
    savings_usd                 NUMERIC(14,8) NOT NULL DEFAULT 0,
    quality_score               DOUBLE PRECISION,
    quality_pass                BOOLEAN,
    quality_scorer              TEXT,
    retried                     BOOLEAN NOT NULL DEFAULT FALSE,
    strategy                    TEXT,
    complexity_score            DOUBLE PRECISION,
    complexity_bucket           TEXT,
    overridden                  BOOLEAN NOT NULL DEFAULT FALSE,
    prev_hash                   TEXT,
    signature                   TEXT,
    signed_by_key_id            TEXT,
    signed_at                   TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_savings_org_created ON routing_savings(org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_savings_alias ON routing_savings(requested_alias);
CREATE INDEX IF NOT EXISTS idx_savings_trace ON routing_savings(trace_id);
CREATE INDEX IF NOT EXISTS idx_savings_recording ON routing_savings(recording_id);
CREATE INDEX IF NOT EXISTS idx_savings_created ON routing_savings(created_at DESC);

-- ---- 5. daily_savings -----------------------------------------------------
-- Roll-up materialized by internal/savings/roller. Used by the dashboard
-- hero stats, the time-series chart, and the eventual "our cut" billing
-- run. our_cut_usd = total_savings_usd * Cfg.SavingsCutPct.
CREATE TABLE IF NOT EXISTS daily_savings (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    date                        DATE NOT NULL,
    org_id                      UUID REFERENCES organizations(id) ON DELETE CASCADE,
    total_requests              INT NOT NULL DEFAULT 0,
    routed_requests             INT NOT NULL DEFAULT 0,
    retry_count                 INT NOT NULL DEFAULT 0,
    total_baseline_cost_usd     NUMERIC(14,6) NOT NULL DEFAULT 0,
    total_actual_cost_usd       NUMERIC(14,6) NOT NULL DEFAULT 0,
    total_savings_usd           NUMERIC(14,6) NOT NULL DEFAULT 0,
    our_cut_usd                 NUMERIC(14,6) NOT NULL DEFAULT 0,
    quality_pass_pct            DOUBLE PRECISION,
    avg_quality_score           DOUBLE PRECISION,
    computed_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_daily_savings
    ON daily_savings(date, COALESCE(org_id::text, ''));
CREATE INDEX IF NOT EXISTS idx_daily_savings_org_date ON daily_savings(org_id, date DESC);

-- +migrate Down
DROP TABLE IF EXISTS daily_savings;
DROP TABLE IF EXISTS routing_savings;
DROP TABLE IF EXISTS routing_policy;
DROP TABLE IF EXISTS org_provider_discounts;
DROP TABLE IF EXISTS operator_price_catalog;
