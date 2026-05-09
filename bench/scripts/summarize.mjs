#!/usr/bin/env node
// summarize.mjs — turn a k6 --summary-export JSON into our canonical schema.
// Canonical schema lives at /bench/schema/benchmark-result.schema.json.
import fs from 'node:fs';
const [name, summaryPath] = process.argv.slice(2);
const raw = JSON.parse(fs.readFileSync(summaryPath, 'utf8'));
const m = raw.metrics || {};
const pick = (metric, stat) => (m[metric] && m[metric][stat] != null ? m[metric][stat] : null);
const result = {
  proxy: name,
  status: 'ok',
  recorded_at: new Date().toISOString(),
  duration_ms: (raw.state && raw.state.testRunDurationMs) || null,
  requests: {
    total:        pick('http_reqs', 'count'),
    rps:          pick('http_reqs', 'rate'),
    failed_rate:  pick('http_req_failed', 'rate'),
  },
  latency_ms: {
    p50: pick('http_req_duration', 'p(50)'),
    p95: pick('http_req_duration', 'p(95)'),
    p99: pick('http_req_duration', 'p(99)'),
    max: pick('http_req_duration', 'max'),
  },
  proxy_overhead_ms: {
    p50: pick('proxy_overhead_ms', 'p(50)'),
    p95: pick('proxy_overhead_ms', 'p(95)'),
    p99: pick('proxy_overhead_ms', 'p(99)'),
  },
};
process.stdout.write(JSON.stringify(result, null, 2));
