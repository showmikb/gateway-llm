-- +migrate Up

-- Cryptographic billing receipts. Every completed inference produces one
-- row. Rows are append-only; billing integrity is enforced by the Ed25519
-- signature stored in `sig` and (optionally) the hash chain in
-- `prev_receipt_id` / `prev_hash`. The raw receipt JSON lives in `body`
-- so verifiers can reconstruct canonical bytes offline.
CREATE TABLE IF NOT EXISTS receipts (
    id              UUID PRIMARY KEY,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    trace_id        TEXT,
    org_id          UUID,
    api_key_id      UUID,
    alias           TEXT NOT NULL,
    provider        TEXT NOT NULL,
    provider_model  TEXT NOT NULL,
    region          TEXT,
    req_hash        TEXT NOT NULL,
    resp_hash       TEXT NOT NULL,
    prompt_tokens   INT NOT NULL DEFAULT 0,
    output_tokens   INT NOT NULL DEFAULT 0,
    total_tokens    INT NOT NULL DEFAULT 0,
    cost_usd        DOUBLE PRECISION NOT NULL DEFAULT 0,
    prev_receipt_id UUID,
    prev_hash       TEXT,
    public_key_id   TEXT NOT NULL,
    sig             TEXT NOT NULL,
    body            JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_receipts_created ON receipts(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_receipts_org ON receipts(org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_receipts_trace ON receipts(trace_id);

-- +migrate Down

DROP TABLE IF EXISTS receipts;
