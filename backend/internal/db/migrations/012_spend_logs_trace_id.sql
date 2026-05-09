-- 012_spend_logs_trace_id.sql
--
-- Threads the request trace_id onto spend_logs so the /usage UI can
-- jump straight into the smart-routing savings drill-down view via
-- GET /v1/management/savings/by-trace/{trace}. Without this column
-- the only way back from "I see a row in usage" to "I want to see
-- baseline-vs-actual cost + signature for this very request" was to
-- open the recordings replay tool, which is too heavy for the
-- common case of "did this request route, and how much did we save?"
--
-- nullable + indexed: pre-existing rows stay valid (NULL trace_id),
-- new rows always populate it; the index keeps the by-trace lookup
-- O(log n) without bloating the existing time-series scans.

ALTER TABLE spend_logs
    ADD COLUMN IF NOT EXISTS trace_id TEXT;

CREATE INDEX IF NOT EXISTS idx_spend_logs_trace_id ON spend_logs(trace_id);
