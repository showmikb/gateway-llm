# Gateway-LLM observability dashboards

Drop-in dashboards for the **Smart Routing Moat** metrics. Both files
expose the same panels (saved $, requests routed, quality pass %,
top aliases, latency P95, retry rate) so a customer can pick whichever
ingestion path they already operate without losing parity.

## Files

| File | Backend | Source signals |
| ---- | ------- | -------------- |
| `grafana-savings.json` | Grafana / Prometheus | scrapes the gateway's `/metrics` endpoint |
| `datadog-savings.json` | Datadog | DogStatsD UDP from the `datadog` callback (`statsd_addr`) **or** Datadog's OTLP intake |

## Metric schema

| Metric (Prometheus → DogStatsD/OTLP) | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `gatewayllm_moat_requests_total` → `gatewayllm.moat.requests` | counter | `alias`, `served_alias`, `strategy`, `retried`, `org_id` | one sample per LLM request that flowed through routing |
| `gatewayllm_moat_baseline_cost_usd_total` → `gatewayllm.moat.baseline_cost_usd` | counter | `alias`, `served_alias`, `org_id` | what the baseline model would have cost |
| `gatewayllm_moat_cost_usd_total` → `gatewayllm.moat.cost_usd` | counter | `alias`, `served_alias`, `org_id` | what the served (routed) request actually cost |
| `gatewayllm_moat_savings_usd_total` → `gatewayllm.moat.savings_usd` | counter | `alias`, `served_alias`, `org_id` | per-request savings (`baseline - actual`) — the moat KPI |
| `gatewayllm_moat_quality_score` → `gatewayllm.moat.quality_score` | histogram | `alias` | judge-assigned quality score, [0,1] |
| `gatewayllm_moat_judge_passes_total` → `gatewayllm.moat.judge_passes` | counter | `alias`, `result` | judge verdicts (`result=pass|fail`) |
| `gatewayllm_moat_routing_decisions_total` → `gatewayllm.moat.routing_decisions` | counter | `decision` | `passthrough` / `overridden` / `retried` |

## Importing

### Grafana

```bash
curl -X POST http://grafana/api/dashboards/db \
  -H "Authorization: Bearer $GRAFANA_TOKEN" \
  -H "Content-Type: application/json" \
  -d @grafana-savings.json
```

Or via the UI: Dashboards → New → Import → upload `grafana-savings.json`.

### Datadog

```bash
curl -X POST "https://api.datadoghq.com/api/v1/dashboard" \
  -H "DD-API-KEY: $DD_API_KEY" \
  -H "DD-APPLICATION-KEY: $DD_APP_KEY" \
  -H "Content-Type: application/json" \
  -d @datadog-savings.json
```

## Wire the gateway

`config.yaml` for Prometheus pull (Grafana path):

```yaml
metrics:
  enabled: true
  path: /metrics
```

For OTLP push (Honeycomb / New Relic / Tempo+Mimir / Grafana Cloud / DD OTLP):

```yaml
metrics:
  enabled: true
  otlp:
    endpoint: https://otlp.example.com
    protocol: http  # or grpc
    headers: { "x-honeycomb-team": "${HONEYCOMB_KEY}" }
    interval: 30s
```

For Datadog DogStatsD:

```yaml
callbacks:
  - type: datadog
    api_key_env: DD_API_KEY
    site: datadoghq.com
    statsd_addr: 127.0.0.1:8125
    statsd_tags: [ "env:prod", "region:us-east-1" ]
```
