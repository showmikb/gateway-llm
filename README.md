# Gateway-LLM -- High-Performance LLM Gateway

A blazing-fast LLM gateway written in Go that provides an OpenAI-compatible API surface with intelligent routing across multiple LLM providers.

## Features

- **OpenAI-compatible API** -- Drop-in replacement for OpenAI's API with support for chat completions, responses API, embeddings, completions, image generation, audio, and moderations
- **Multi-provider routing** -- Route requests to OpenAI, Anthropic, and Google Gemini with automatic translation between formats
- **Streaming support** -- Full SSE streaming for chat completions and responses API
- **Responses API bridge** -- Providers without native Responses API support are automatically bridged through chat completions
- **Load balancing** -- Round-robin and least-latency routing strategies across multiple deployments
- **Automatic retries and fallbacks** -- Configurable retry logic with fallback to alternative deployments on failure
- **Virtual API keys** -- Issue and manage API keys with per-key rate limits, model access control, and budget caps
- **Rate limiting** -- Per-key RPM/TPM enforcement via Redis (with in-memory fallback)
- **Cost tracking** -- Built-in pricing database with per-request cost calculation and custom pricing overrides
- **Spend logging** -- Async per-request spend logging with daily aggregation
- **Observability** -- Prometheus `/metrics`, OTLP traces (Datadog APM, Honeycomb, Tempo, Jaeger), Datadog Logs, Langfuse, Splunk, Slack — see [docs/observability.md](docs/observability.md)
- **Admin UI** -- Separate Next.js dashboard for managing keys, models, pricing, and usage
- **Docker-ready** -- Multi-stage Dockerfiles for tiny (~20MB) production images

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   Client (OpenAI SDK / curl)         │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│                 Gateway-LLM (Go)                │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐  │
│  │   Auth   │→ │Rate Limit │→ │   Model Router   │  │
│  └──────────┘  └───────────┘  └────────┬─────────┘  │
│                                        │             │
│  ┌─────────────────────────────────────▼──────────┐  │
│  │            Provider Translators                │  │
│  │  ┌─────────┐  ┌───────────┐  ┌─────────────┐  │  │
│  │  │ OpenAI  │  │ Anthropic │  │   Gemini    │  │  │
│  │  └─────────┘  └───────────┘  └─────────────┘  │  │
│  └────────────────────────────────────────────────┘  │
│                                                      │
│  ┌──────────────┐  ┌─────────┐  ┌──────────────┐    │
│  │  Cost Engine │  │  Redis  │  │  PostgreSQL  │    │
│  └──────────────┘  └─────────┘  └──────────────┘    │
└──────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.23+
- PostgreSQL 16+
- Redis 7+ (optional, for distributed rate limiting)
- Node.js 22+ (for the admin UI)

### 1. Configure

```bash
cd gateway-llm
cp .env.example .env
```

Edit `.env` and add your provider API keys:

```
GATEWAY_LLM_MASTER_KEY=sk-master-your-secret-key
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GEMINI_API_KEY=AIza...
```

The database and Redis URLs have sane defaults for the Docker Compose setup and don't need changes.

### 2. Start everything

```bash
docker compose up -d
```

This starts the backend (`:8080`), admin UI (`:3000`), PostgreSQL, and Redis.
Open **http://localhost:3000** and enter your master key to access the admin dashboard.

### 3. Run locally (development)

```bash
# Start the backend
cd backend
go run ./cmd/gateway-llm --config ../config.yaml

# In another terminal, start the UI
cd ui
npm install
npm run dev
```

### 4. Test it

```bash
# Chat completion
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $GATEWAY_LLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# Streaming
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $GATEWAY_LLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet",
    "messages": [{"role": "user", "content": "Tell me a joke"}],
    "stream": true
  }'

# Responses API
curl http://localhost:8080/v1/responses \
  -H "Authorization: Bearer $GATEWAY_LLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smart",
    "input": "What is the capital of France?"
  }'

# Embeddings
curl http://localhost:8080/v1/embeddings \
  -H "Authorization: Bearer $GATEWAY_LLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "text-embedding-3-small",
    "input": "Hello world"
  }'

# List models
curl http://localhost:8080/v1/models \
  -H "Authorization: Bearer $GATEWAY_LLM_MASTER_KEY"
```

## API Endpoints

### LLM Endpoints (OpenAI-compatible)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/chat/completions` | Chat completions (streaming supported) |
| POST | `/v1/responses` | Responses API (streaming supported) |
| GET | `/v1/responses/{id}` | Retrieve a response |
| DELETE | `/v1/responses/{id}` | Delete a response |
| POST | `/v1/embeddings` | Generate embeddings |
| POST | `/v1/completions` | Legacy text completions |
| POST | `/v1/images/generations` | Image generation |
| POST | `/v1/audio/speech` | Text-to-speech |
| POST | `/v1/audio/transcriptions` | Speech-to-text |
| POST | `/v1/moderations` | Content moderation |
| GET | `/v1/models` | List available models |
| GET | `/health` | Liveness check |
| GET | `/health/ready` | Readiness check |

### Management Endpoints (admin-only)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/management/keys` | Create a virtual API key |
| GET | `/v1/management/keys` | List all API keys |
| DELETE | `/v1/management/keys/{id}` | Delete an API key |
| POST | `/v1/management/teams` | Create a team |
| GET | `/v1/management/teams` | List all teams |
| DELETE | `/v1/management/teams/{id}` | Delete a team |
| GET | `/v1/management/usage` | Get spend logs |
| GET | `/v1/management/usage/daily` | Get daily aggregated spend |
| GET | `/v1/management/deployments` | List model deployments |
| GET | `/v1/management/pricing` | List all pricing (built-in + custom) |
| PUT | `/v1/management/pricing/{provider}/{model}` | Set custom pricing |
| DELETE | `/v1/management/pricing/{provider}/{model}` | Remove custom pricing |

## Configuration

Configuration is via a YAML file with environment variable substitution (`${VAR_NAME}`):

```yaml
server:
  port: 8080
  read_timeout: 30s
  write_timeout: 120s

database:
  url: ${DATABASE_URL}
  max_connections: 25

redis:
  url: ${REDIS_URL}

auth:
  master_key: ${GATEWAY_LLM_MASTER_KEY}

model_list:
  - model_alias: "smart"            # User-facing name
    deployments:
      - provider: openai            # openai | anthropic | gemini
        model: gpt-4o               # Actual model name at provider
        api_key_env: OPENAI_API_KEY  # Env var with the API key
      - provider: anthropic
        model: claude-sonnet-4-20250514
        api_key_env: ANTHROPIC_API_KEY

routing:
  strategy: round-robin   # round-robin | least-latency
  retries: 2
  retry_delay: 500ms
  fallback_enabled: true

rate_limiting:
  default_rpm: 60
  default_tpm: 100000
  window: 1m

logging:
  level: info             # debug | info | warn | error
  format: json
```

## Provider Support

| Feature | OpenAI | Anthropic | Gemini |
|---------|--------|-----------|--------|
| Chat Completions | Native | Translated | Translated |
| Responses API | Native | Via Bridge | Via Bridge |
| Embeddings | Native | -- | Native |
| Legacy Completions | Native | Via Chat | Via Chat |
| Image Generation | Native | -- | -- |
| Audio Speech | Native | -- | -- |
| Audio Transcription | Native | -- | -- |
| Moderations | Native | -- | -- |

## Cost Tracking

Every request is priced using a built-in pricing database. Custom pricing can be set via the management API:

```bash
# Set custom pricing for a model
curl -X PUT http://localhost:8080/v1/management/pricing/openai/gpt-4o \
  -H "Authorization: Bearer $GATEWAY_LLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "input_cost_per_token": 3.0e-06,
    "output_cost_per_token": 1.2e-05
  }'

# View all pricing
curl http://localhost:8080/v1/management/pricing \
  -H "Authorization: Bearer $GATEWAY_LLM_MASTER_KEY"
```

Cost headers are included in every response:
- `X-Gateway-LLM-Cost` -- Total cost in USD
- `X-Gateway-LLM-Tokens-Input` -- Input tokens used
- `X-Gateway-LLM-Tokens-Output` -- Output tokens used

## Observability

Gateway-LLM exposes two complementary export paths for any observability
stack:

- `GET /metrics` — Prometheus scrape with request, latency, token, cost,
  cache, guardrail, rate-limit, and SmartRoute counters. Disable via
  `metrics.enabled: false` in `config.yaml`.
- `callbacks:` — push exporters for Datadog Logs, OTLP traces (Datadog
  APM, Honeycomb, Tempo, Jaeger), Langfuse, Splunk, Slack, generic
  webhooks, and more.

See [docs/observability.md](docs/observability.md) for vendor-by-vendor
configuration recipes.

## Project Structure

```
gateway-llm/
  backend/                    Go backend
    cmd/gateway-llm/main.go      Entrypoint
    internal/
      config/                  YAML config loader
      server/                  HTTP server + handlers + middleware
      router/                  Model routing + fallback
      providers/               Provider translators (OpenAI, Anthropic, Gemini)
      cost/                    Cost calculation engine + pricing DB
      types/                   Shared OpenAI-format types
      models/                  Domain models
      db/                      PostgreSQL layer + migrations
      cache/                   Redis wrapper
  ui/                          Next.js admin dashboard
  docker/                      Dockerfiles
  docker-compose.yml           Full stack orchestration
  config.example.yaml          Example configuration
```

## License

MIT
