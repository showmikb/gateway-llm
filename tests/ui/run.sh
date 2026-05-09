#!/usr/bin/env bash
# Gateway-LLM UI test toolkit runner.
# Defaults point at production (app.gateway-llm.com / api.gateway-llm.com).
# Override with UI_URL / API_URL / MASTER_KEY env vars to target staging.
#
# Usage:
#   ./run.sh                # runs the whole suite
#   ./run.sh 00-smoke       # grep filter passed through to Playwright
#   ./run.sh --headed 04    # interactive run of the user-journey spec
set -euo pipefail

cd "$(dirname "$0")"

if [ ! -d node_modules ]; then
  echo "[run.sh] installing dependencies..."
  npm install --silent
fi

# Install the Chromium bundle Playwright expects. This is a no-op after
# the first run but keeps fresh checkouts self-healing.
if [ ! -d "$HOME/.cache/ms-playwright" ] && [ ! -d "$HOME/Library/Caches/ms-playwright" ]; then
  echo "[run.sh] installing Playwright browsers..."
  npx playwright install chromium >/dev/null
fi

echo "[run.sh] UI=${UI_URL:-https://app.gateway-llm.com}"
echo "[run.sh] API=${API_URL:-https://api.gateway-llm.com}"

npx playwright test "$@"
status=$?

if [ $status -ne 0 ]; then
  echo
  echo "[run.sh] Tests failed. Open the HTML report with:"
  echo "         npx playwright show-report"
fi

exit $status
