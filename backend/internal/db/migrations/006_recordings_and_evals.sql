-- +migrate Up

-- Recordings are the per-request ground truth: the canonical IR of the
-- request, the canonical IR of the response, provider + model chosen, and
-- a blob reference to the full payloads in S3/MinIO/local disk. We keep
-- payload-sized fields out of Postgres to avoid bloating rows; a blob
-- reference URL is enough for replay.
CREATE TABLE IF NOT EXISTS recordings (
    id              UUID PRIMARY KEY,
    trace_id        TEXT,
    api_key_id      UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    user_id         UUID REFERENCES users(id) ON DELETE SET NULL,
    team_id         UUID REFERENCES teams(id) ON DELETE SET NULL,
    org_id          UUID REFERENCES organizations(id) ON DELETE SET NULL,
    model_alias     TEXT NOT NULL,
    provider        TEXT NOT NULL,
    provider_model  TEXT NOT NULL,
    endpoint        TEXT NOT NULL,
    ingress         TEXT,                       -- which SDK format the request arrived in
    status_code     INT,
    prompt_tokens   INT,
    completion_tokens INT,
    total_tokens    INT,
    cost_usd        DOUBLE PRECISION,
    latency_ms      INT,
    router_reason   TEXT,                       -- why SmartRouter picked this deployment
    eval_score      DOUBLE PRECISION,           -- optional inline eval score
    tags            TEXT[],
    metadata        JSONB,
    request_blob    TEXT,                       -- s3://… or file://… URL to the full request JSON
    response_blob   TEXT,
    request_hash    TEXT NOT NULL,              -- sha256 of canonical IR — used as dedup/replay key
    response_hash   TEXT,
    redacted        BOOLEAN DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recordings_api_key_id ON recordings(api_key_id);
CREATE INDEX IF NOT EXISTS idx_recordings_team_id ON recordings(team_id);
CREATE INDEX IF NOT EXISTS idx_recordings_org_id ON recordings(org_id);
CREATE INDEX IF NOT EXISTS idx_recordings_created_at ON recordings(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_recordings_model_alias ON recordings(model_alias);
CREATE INDEX IF NOT EXISTS idx_recordings_request_hash ON recordings(request_hash);
CREATE INDEX IF NOT EXISTS idx_recordings_trace_id ON recordings(trace_id);
CREATE INDEX IF NOT EXISTS idx_recordings_tags ON recordings USING GIN(tags);

-- Replay runs group a batch of replays (e.g. "run every recording from last
-- week against claude-sonnet-4-5 and score them"). Results per-recording
-- land in `replay_results`.
CREATE TABLE IF NOT EXISTS replay_runs (
    id              UUID PRIMARY KEY,
    name            TEXT NOT NULL,
    target_alias    TEXT,                       -- which model alias to replay against
    scorer          TEXT,                       -- "llm_judge", "json_schema", etc.
    scorer_config   JSONB,
    created_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    org_id          UUID REFERENCES organizations(id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending|running|completed|failed
    total           INT DEFAULT 0,
    completed       INT DEFAULT 0,
    failed          INT DEFAULT 0,
    avg_score       DOUBLE PRECISION,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_replay_runs_org_id ON replay_runs(org_id);
CREATE INDEX IF NOT EXISTS idx_replay_runs_status ON replay_runs(status);

CREATE TABLE IF NOT EXISTS replay_results (
    id              UUID PRIMARY KEY,
    run_id          UUID NOT NULL REFERENCES replay_runs(id) ON DELETE CASCADE,
    recording_id    UUID REFERENCES recordings(id) ON DELETE SET NULL,
    target_alias    TEXT,
    status          TEXT NOT NULL,
    score           DOUBLE PRECISION,
    cost_usd        DOUBLE PRECISION,
    latency_ms      INT,
    response_blob   TEXT,
    response_hash   TEXT,
    eval_detail     JSONB,
    error           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_replay_results_run_id ON replay_results(run_id);
CREATE INDEX IF NOT EXISTS idx_replay_results_recording_id ON replay_results(recording_id);

-- Online eval scores. Every recording can accumulate multiple scores from
-- different scorers; we keep them in a separate table so scorers can be
-- added or re-run without rewriting recordings.
CREATE TABLE IF NOT EXISTS eval_scores (
    id              UUID PRIMARY KEY,
    recording_id    UUID NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    scorer          TEXT NOT NULL,
    score           DOUBLE PRECISION,
    pass            BOOLEAN,
    detail          JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_eval_scores_recording_id ON eval_scores(recording_id);
CREATE INDEX IF NOT EXISTS idx_eval_scores_scorer ON eval_scores(scorer);

-- +migrate Down

DROP TABLE IF EXISTS eval_scores;
DROP TABLE IF EXISTS replay_results;
DROP TABLE IF EXISTS replay_runs;
DROP TABLE IF EXISTS recordings;
