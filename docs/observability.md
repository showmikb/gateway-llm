# Observability

Gateway-LLM ships first-class export paths so you can plug it into whatever
observability stack you already run. Today there are two complementary
surfaces:

1. **Prometheus** at `GET /metrics` — pull-based, label-rich,
   vendor-agnostic.
2. **Callback exporters** — push-based, one event per request, ideal for
   APM/log/trace backends (Datadog, Honeycomb, Tempo, Langfuse, Splunk,
   Slack, …).

Pick whichever matches the receiving system. Most operators end up using
both: Prometheus for live RED dashboards, callbacks for per-request
traces and spend records.

## 1. Prometheus `/metrics`

The gateway publishes a Prometheus scrape endpoint by default. No auth
is required — bind it on an internal listener if your environment
forbids unauthenticated endpoints, or disable via config.

```yaml
# config.yaml
metrics:
  enabled: true     # default: true
  path: /metrics    # default: /metrics
```

### Exposed series

All series are namespaced under `gatewayllm_`:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `gatewayllm_requests_total` | counter | `model`, `provider`, `status` (`2xx`/`4xx`/`5xx`/…) | One increment per completed request. |
| `gatewayllm_request_duration_seconds` | histogram | `model`, `provider` | End-to-end latency, buckets `[0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120]`. |
| `gatewayllm_tokens_total` | counter | `model`, `provider`, `kind` (`prompt`/`completion`) | Tokens billed by upstream. |
| `gatewayllm_cost_usd_total` | counter | `model`, `provider` | Cumulative USD spend. |
| `gatewayllm_cache_hit_total` | counter | `type` (`exact`/`semantic`) | Cache hits served. |
| `gatewayllm_cache_miss_total` | counter | `type` | Cache misses. |
| `gatewayllm_guardrail_block_total` | counter | `reason` | Requests blocked by the guardrails engine. |
| `gatewayllm_rate_limit_hit_total` | counter | `kind` (`rpm`/`tpm`) | 429s emitted by the rate limiter. |
| `gatewayllm_smart_route_decisions_total` | counter | `bucket`, `override` | SmartRoute classifications. |
| `gatewayllm_circuit_breaker_open` | gauge | `deployment`, `provider` | 1 when a deployment's breaker is open. |

Standard `go_*` and `process_*` collectors are also registered.

### Datadog Agent (OpenMetrics)

```yaml
# /etc/datadog-agent/conf.d/openmetrics.d/conf.yaml
init_config:
instances:
  - openmetrics_endpoint: http://gatewayllm:8080/metrics
    namespace: gatewayllm
    metrics:
      - "gatewayllm_*"
```

### Grafana Agent / Mimir

```yaml
metrics:
  configs:
    - name: gateway-llm
      scrape_configs:
        - job_name: gateway-llm
          static_configs:
            - targets: ["gatewayllm:8080"]
          metrics_path: /metrics
```

### Plain Prometheus

```yaml
scrape_configs:
  - job_name: gateway-llm
    static_configs:
      - targets: ["gatewayllm:8080"]
```

## 2. Callback exporters

Callbacks push one `RequestEvent` per finished request to an external
system. Configure them under `callbacks:` in `config.yaml`. Multiple
callbacks can be active simultaneously; each runs in its own goroutine
and never blocks the request hot path.

### Datadog Logs

```yaml
callbacks:
  - type: datadog
    endpoint: https://http-intake.logs.datadoghq.com/api/v2/logs
    api_key_env: ${DD_API_KEY}
    site: datadoghq.com
```

### OpenTelemetry / OTLP (recommended for Datadog APM, Honeycomb, Tempo, Jaeger)

`otel_otlp` uses the official OpenTelemetry Go SDK and exports proper
OTLP traces. Each chat completion becomes one client span with
`gen_ai.*` semantic-convention attributes plus the gateway's own
`gateway_llm.cost_usd`, `gateway_llm.api_key_id`, etc.

```yaml
callbacks:
  - type: otel_otlp
    endpoint: https://api.honeycomb.io/v1/traces   # or http://otel-collector:4318/v1/traces
    protocol: http                                   # http | grpc
    service_name: gateway-llm
    timeout: 10s
    headers:
      x-honeycomb-team: ${HONEYCOMB_API_KEY}
```

For Datadog APM, point `endpoint` at your Datadog Agent's OTLP intake
(`http://datadog-agent:4318/v1/traces`) and add the
`dd-api-key` header. The legacy `otel` callback (hand-rolled OTLP-JSON
HTTP) still works but is deprecated; switch to `otel_otlp` when you
can.

### Langfuse (LLM tracing)

```yaml
callbacks:
  - type: langfuse
    endpoint: https://cloud.langfuse.com
    public_key_env: ${LANGFUSE_PUBLIC_KEY}
    secret_key_env: ${LANGFUSE_SECRET_KEY}
```

### Splunk HEC

```yaml
callbacks:
  - type: splunk
    endpoint: https://splunk.example.com:8088/services/collector/event
    token_env: ${SPLUNK_HEC_TOKEN}
    index: gateway_llm
    source: gateway-llm
```

### Slack (high-cost request alerts)

```yaml
callbacks:
  - type: slack
    endpoint: ${SLACK_WEBHOOK_URL}
    min_cost_usd: 1.00   # only alert on requests >= $1.00
```

### LangSmith / Arize / Helicone / generic webhook

These callbacks are wired through the same `callbacks:` block; see the
`callback.go` interface and `factory.go` for the available fields per
type.

## Quick recipe matrix

| Target | What to use |
|---|---|
| Datadog metrics | `/metrics` via Datadog Agent OpenMetrics |
| Datadog logs | `type: datadog` callback |
| Datadog APM | `type: otel_otlp` to Datadog Agent OTLP |
| Honeycomb | `type: otel_otlp` (HTTP) |
| Tempo / Jaeger | `type: otel_otlp` (gRPC or HTTP) |
| Grafana Cloud Metrics | `/metrics` scrape |
| Prometheus / Mimir | `/metrics` scrape |
| Splunk Observability | `/metrics` scrape via OTel collector |
| New Relic | `/metrics` scrape (OpenMetrics integration) |
| Langfuse | `type: langfuse` callback |
| Splunk Enterprise | `type: splunk` callback |
| PagerDuty / Slack ops | `type: slack` + `min_cost_usd` |

## What's intentionally out of scope

- **Persistent counters across restarts** — Prometheus is pull-based and
  rate() handles counter resets correctly. The `daily_spend` rollup in
  Postgres is the durable record.
- **Per-key auth on `/metrics`** — by design, `/metrics` follows the
  same pattern as `/healthz`. Bind on an internal listener or front it
  with a sidecar if you need policy.
