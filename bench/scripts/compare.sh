#!/usr/bin/env bash
# compare.sh — run the same k6 scenario against gateway-llm, LiteLLM, Bifrost,
# and Portkey in identical containers, then emit a machine-readable result
# JSON that the website's /evidence/benchmarks page renders.
#
# Any proxy that fails to start is recorded as "not_available" rather than
# blocking the whole run; that way a single broken image doesn't block CI.

set -euo pipefail

cd "$(dirname "$0")/.."

OUT_DIR="${OUT_DIR:-./results}"
mkdir -p "$OUT_DIR"

MOCKLLM_PORT=${MOCKLLM_PORT:-9090}
DURATION=${DURATION:-30s}
VUS=${VUS:-50}

# Start mockllm in the background.
go run ./mockllm -addr ":$MOCKLLM_PORT" -delay 50ms &
MOCK_PID=$!
trap "kill $MOCK_PID 2>/dev/null || true" EXIT

# Each proxy is started on its own port; we point k6 at each in turn.
declare -A PROXIES=(
  [gateway-llm]=8080
  [litellm]=8081
  [bifrost]=8082
  [portkey]=8083
)

run_k6() {
  local name="$1"
  local port="$2"
  local url="http://localhost:$port"
  # Health probe; skip if not running.
  if ! curl -s "$url/health" -o /dev/null; then
    if ! curl -s "$url/" -o /dev/null; then
      echo "$name not reachable at $url — skipping"
      echo "{\"proxy\":\"$name\",\"status\":\"not_available\"}" \
        > "$OUT_DIR/${name}.json"
      return
    fi
  fi
  BASE_URL="$url" DURATION="$DURATION" VUS="$VUS" k6 run \
    --out json="$OUT_DIR/${name}.k6.json" \
    --summary-export "$OUT_DIR/${name}.summary.json" \
    ./k6/chat.js || true
  node ./scripts/summarize.mjs "$name" \
    "$OUT_DIR/${name}.summary.json" \
    > "$OUT_DIR/${name}.json"
}

for name in "${!PROXIES[@]}"; do
  run_k6 "$name" "${PROXIES[$name]}"
done

# Merge into one machine-readable file for the website.
node ./scripts/merge.mjs "$OUT_DIR" > "$OUT_DIR/all.json"
echo "wrote $OUT_DIR/all.json"
