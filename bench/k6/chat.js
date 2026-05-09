// k6 scenario used in every gateway-llm perf comparison.
//
// Environment inputs:
//   BASE_URL   - base URL of the proxy under test (default http://localhost:8080)
//   API_KEY    - bearer token (default "sk-test")
//   MODEL      - model alias to request (default "gpt-4o")
//   DURATION   - test duration (default 60s)
//   VUS        - concurrent virtual users (default 50)
//   STREAM     - "true" to exercise the streaming path

import http from 'k6/http';
import { check } from 'k6';
import { Trend } from 'k6/metrics';

const baseURL = __ENV.BASE_URL || 'http://localhost:8080';
const apiKey  = __ENV.API_KEY  || 'sk-test';
const model   = __ENV.MODEL    || 'gpt-4o';
const stream  = __ENV.STREAM === 'true';
const vus     = parseInt(__ENV.VUS      || '50', 10);
const dur     = __ENV.DURATION || '60s';

export const options = {
  scenarios: {
    constant: { executor: 'constant-vus', vus, duration: dur },
  },
  thresholds: {
    // Hard ceilings. If we can't clear these against mockllm, something is
    // wrong with the proxy. These are NOT the same as publicly advertised
    // numbers — they are a safety net.
    http_req_duration: ['p(99)<500', 'p(95)<150', 'p(50)<60'],
    http_req_failed:   ['rate<0.01'],
  },
};

const proxyOverhead = new Trend('proxy_overhead_ms');

export default function () {
  const body = {
    model,
    messages: [{ role: 'user', content: 'hello' }],
    stream,
  };
  const res = http.post(`${baseURL}/v1/chat/completions`, JSON.stringify(body), {
    headers: {
      'Content-Type':  'application/json',
      'Authorization': `Bearer ${apiKey}`,
    },
  });
  check(res, {
    '200 OK':    (r) => r.status === 200,
    'has_body':  (r) => r.body && r.body.length > 0,
  });
  // The proxy exposes its own latency as an X-header so we can subtract
  // upstream time and get proxy overhead directly.
  const upstream = parseInt(res.headers['X-Gateway-Llm-Upstream-Ms'] || '0', 10);
  if (!isNaN(upstream) && res.timings.duration > 0) {
    proxyOverhead.add(res.timings.duration - upstream);
  }
}
