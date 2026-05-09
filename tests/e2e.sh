#!/usr/bin/env bash
set -euo pipefail

# Gateway-LLM End-to-End Test Suite
# Usage: ./tests/e2e.sh [BASE_URL] [MASTER_KEY]

BASE="${1:-http://localhost:8080}"
KEY="${2:-sk-master-change-me}"
PASS=0
FAIL=0
TOTAL=0

green() { printf "\033[32m%s\033[0m\n" "$1"; }
red()   { printf "\033[31m%s\033[0m\n" "$1"; }
bold()  { printf "\033[1m%s\033[0m\n" "$1"; }

check() {
  local desc="$1" expected_status="$2"
  shift 2
  TOTAL=$((TOTAL + 1))
  local http_code body
  body=$(curl -s -o /dev/null -w "%{http_code}" "$@") || true
  http_code="$body"
  body=$(curl -s "$@") || true

  if [ "$http_code" = "$expected_status" ]; then
    PASS=$((PASS + 1))
    green "  PASS: $desc (HTTP $http_code)"
  else
    FAIL=$((FAIL + 1))
    red "  FAIL: $desc (expected $expected_status, got $http_code)"
    echo "    Response: ${body:0:200}"
  fi
}

check_json_field() {
  local desc="$1" expected_status="$2" field="$3"
  shift 3
  TOTAL=$((TOTAL + 1))
  local resp http_code
  resp=$(curl -s -w "\n%{http_code}" "$@") || true
  http_code=$(echo "$resp" | tail -1)
  body=$(echo "$resp" | sed '$d')

  if [ "$http_code" != "$expected_status" ]; then
    FAIL=$((FAIL + 1))
    red "  FAIL: $desc (expected HTTP $expected_status, got $http_code)"
    echo "    Response: ${body:0:200}"
    return
  fi

  if echo "$body" | grep -q "$field"; then
    PASS=$((PASS + 1))
    green "  PASS: $desc"
  else
    FAIL=$((FAIL + 1))
    red "  FAIL: $desc (field '$field' not found)"
    echo "    Response: ${body:0:200}"
  fi
}

AUTH="-H Authorization:\ Bearer\ $KEY"
RUN_ID=$(date +%s)

bold "=== Gateway-LLM E2E Test Suite ==="
echo "Base URL: $BASE"
echo "Master Key: ${KEY:0:8}..."
echo ""

# ---- Root & Docs ----
bold "--- Root & Documentation ---"
check "GET / returns 200" "200" "$BASE/"
check_json_field "GET / returns API info" "200" '"name"' "$BASE/"
check "GET /docs returns 200 (Swagger UI)" "200" "$BASE/docs"
check "GET /docs/openapi.yaml returns 200" "200" "$BASE/docs/openapi.yaml"

# ---- Health ----
bold "--- Health Endpoints ---"
check "GET /health returns 200" "200" "$BASE/health"
check "GET /health/ready returns 200" "200" "$BASE/health/ready"

# ---- Auth ----
bold "--- Authentication ---"
check "No token -> 401" "401" "$BASE/v1/models"
check "Invalid token -> 401" "401" -H "Authorization: Bearer invalid-key-12345" "$BASE/v1/models"
check "Master key -> 200" "200" -H "Authorization: Bearer $KEY" "$BASE/v1/models"

# ---- Models ----
bold "--- Models ---"
check_json_field "GET /v1/models returns list" "200" '"object"' -H "Authorization: Bearer $KEY" "$BASE/v1/models"
check "GET /v1/models/nonexistent -> 404" "404" -H "Authorization: Bearer $KEY" "$BASE/v1/models/nonexistent"

# ---- Management: API Keys ----
bold "--- API Key Management ---"
KEY_RESP=$(curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"name":"e2e-test-key","rpm_limit":100,"tpm_limit":50000,"max_budget":10.0}' "$BASE/v1/management/keys")
KEY_ID=$(echo "$KEY_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
RAW_KEY=$(echo "$KEY_RESP" | grep -o '"key":"[^"]*"' | head -1 | cut -d'"' -f4 || true)

TOTAL=$((TOTAL + 1))
if [ -n "$KEY_ID" ] && [ -n "$RAW_KEY" ]; then
  PASS=$((PASS + 1))
  green "  PASS: Create API key with limits (id=$KEY_ID)"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: Create API key"
  echo "    Response: ${KEY_RESP:0:200}"
fi

check_json_field "List API keys" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/keys"

if [ -n "$RAW_KEY" ]; then
  check "Created key can access /v1/models" "200" -H "Authorization: Bearer $RAW_KEY" "$BASE/v1/models"
  check "Non-master key -> 403 on management" "403" -H "Authorization: Bearer $RAW_KEY" "$BASE/v1/management/keys"
fi

# Update key
if [ -n "$KEY_ID" ]; then
  UPDATE_KEY_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    -d '{"name":"e2e-test-key-updated","is_active":false}' "$BASE/v1/management/keys/$KEY_ID")
  TOTAL=$((TOTAL + 1))
  if [ "$UPDATE_KEY_CODE" = "200" ]; then
    PASS=$((PASS + 1))
    green "  PASS: Update API key"
  else
    FAIL=$((FAIL + 1))
    red "  FAIL: Update API key (HTTP $UPDATE_KEY_CODE)"
  fi
fi

if [ -n "$KEY_ID" ]; then
  check "Delete API key" "204" -X DELETE -H "Authorization: Bearer $KEY" "$BASE/v1/management/keys/$KEY_ID"
fi

# ---- Management: Teams ----
bold "--- Team Management ---"
TEAM_RESP=$(curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"name":"e2e-test-team"}' "$BASE/v1/management/teams")
TEAM_ID=$(echo "$TEAM_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)

TOTAL=$((TOTAL + 1))
if [ -n "$TEAM_ID" ]; then
  PASS=$((PASS + 1))
  green "  PASS: Create team (id=$TEAM_ID)"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: Create team"
  echo "    Response: ${TEAM_RESP:0:200}"
fi

check_json_field "List teams" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/teams"

if [ -n "$TEAM_ID" ]; then
  check "Delete team (cascade keys)" "204" -X DELETE -H "Authorization: Bearer $KEY" "$BASE/v1/management/teams/$TEAM_ID"
fi

# ---- Management: Organizations ----
bold "--- Organization Management ---"
ORG_RESP=$(curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"name":"E2E Test Org","max_budget":5000}' "$BASE/v1/management/organizations")
ORG_ID=$(echo "$ORG_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
ORG_SLUG=$(echo "$ORG_RESP" | grep -o '"slug":"[^"]*"' | head -1 | cut -d'"' -f4 || true)

TOTAL=$((TOTAL + 1))
if [ -n "$ORG_ID" ]; then
  PASS=$((PASS + 1))
  green "  PASS: Create organization (id=$ORG_ID, slug=$ORG_SLUG)"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: Create organization"
  echo "    Response: ${ORG_RESP:0:200}"
fi

check_json_field "List organizations" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/organizations"

# Update org
if [ -n "$ORG_ID" ]; then
  UPDATE_ORG_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    -d '{"name":"E2E Org Updated"}' "$BASE/v1/management/organizations/$ORG_ID")
  TOTAL=$((TOTAL + 1))
  if [ "$UPDATE_ORG_CODE" = "200" ]; then
    PASS=$((PASS + 1))
    green "  PASS: Update organization"
  else
    FAIL=$((FAIL + 1))
    red "  FAIL: Update organization (HTTP $UPDATE_ORG_CODE)"
  fi

  check "Delete organization" "204" -X DELETE -H "Authorization: Bearer $KEY" "$BASE/v1/management/organizations/$ORG_ID"
fi

# ---- Management: Provider Credentials ----
bold "--- Provider Credentials ---"
CRED_RESP=$(curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"name":"e2e-openai-key","provider":"openai","api_key":"sk-test-fake-key-12345"}' \
  "$BASE/v1/management/credentials")
CRED_ID=$(echo "$CRED_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
CRED_MASKED=$(echo "$CRED_RESP" | grep -o '"api_key_masked":"[^"]*"' | head -1 | cut -d'"' -f4 || true)

TOTAL=$((TOTAL + 1))
if [ -n "$CRED_ID" ] && [ -n "$CRED_MASKED" ]; then
  PASS=$((PASS + 1))
  green "  PASS: Create credential (id=$CRED_ID, masked=$CRED_MASKED)"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: Create credential"
  echo "    Response: ${CRED_RESP:0:200}"
fi

check_json_field "List credentials" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/credentials"

# Update credential
if [ -n "$CRED_ID" ]; then
  UPDATE_CRED_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    -d '{"name":"e2e-openai-key-updated"}' "$BASE/v1/management/credentials/$CRED_ID")
  TOTAL=$((TOTAL + 1))
  if [ "$UPDATE_CRED_CODE" = "200" ]; then
    PASS=$((PASS + 1))
    green "  PASS: Update credential"
  else
    FAIL=$((FAIL + 1))
    red "  FAIL: Update credential (HTTP $UPDATE_CRED_CODE)"
  fi

  # Test credential (will fail since key is fake, but endpoint should respond)
  check_json_field "Test credential (returns status)" "200" '"status"' \
    -X POST -H "Authorization: Bearer $KEY" "$BASE/v1/management/credentials/$CRED_ID/test"

  check "Delete credential" "204" -X DELETE -H "Authorization: Bearer $KEY" "$BASE/v1/management/credentials/$CRED_ID"
fi

# ---- Management: Deployments CRUD ----
bold "--- Deployment CRUD ---"
DEP_RESP=$(curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"model_alias":"e2e-test-model","provider":"openai","provider_model":"gpt-4o","api_key_env":"OPENAI_API_KEY","priority":1}' \
  "$BASE/v1/management/deployments")
DEP_ID=$(echo "$DEP_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)

TOTAL=$((TOTAL + 1))
if [ -n "$DEP_ID" ]; then
  PASS=$((PASS + 1))
  green "  PASS: Create deployment (id=$DEP_ID)"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: Create deployment"
  echo "    Response: ${DEP_RESP:0:200}"
fi

check_json_field "List deployments" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/deployments"

if [ -n "$DEP_ID" ]; then
  UPDATE_DEP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    -d '{"is_active":false}' "$BASE/v1/management/deployments/$DEP_ID")
  TOTAL=$((TOTAL + 1))
  if [ "$UPDATE_DEP_CODE" = "200" ]; then
    PASS=$((PASS + 1))
    green "  PASS: Update deployment (deactivate)"
  else
    FAIL=$((FAIL + 1))
    red "  FAIL: Update deployment (HTTP $UPDATE_DEP_CODE)"
  fi

  check "Delete deployment" "204" -X DELETE -H "Authorization: Bearer $KEY" "$BASE/v1/management/deployments/$DEP_ID"
fi

# ---- Management: Users ----
bold "--- User Management ---"
E2E_EMAIL="e2e-${RUN_ID}@test.com"
USER_RESP=$(curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d "{\"email\":\"$E2E_EMAIL\",\"password\":\"testpass123\",\"role\":\"member\"}" "$BASE/v1/management/users")
USER_ID=$(echo "$USER_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)

TOTAL=$((TOTAL + 1))
if [ -n "$USER_ID" ]; then
  PASS=$((PASS + 1))
  green "  PASS: Create user (id=$USER_ID)"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: Create user"
  echo "    Response: ${USER_RESP:0:200}"
fi

check_json_field "List users" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/users"

if [ -n "$USER_ID" ]; then
  check_json_field "Get user by ID" "200" '"email"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/users/$USER_ID"

  # Update role
  UPDATE_RESP=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
    -d '{"role":"org_admin"}' "$BASE/v1/management/users/$USER_ID")
  TOTAL=$((TOTAL + 1))
  if [ "$UPDATE_RESP" = "200" ]; then
    PASS=$((PASS + 1))
    green "  PASS: Update user role to org_admin"
  else
    FAIL=$((FAIL + 1))
    red "  FAIL: Update user role (HTTP $UPDATE_RESP)"
  fi

  # Login with email/password
  LOGIN_RESP=$(curl -s "$BASE/v1/management/users/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$E2E_EMAIL\",\"password\":\"testpass123\"}")
  SESSION_TOKEN=$(echo "$LOGIN_RESP" | grep -o '"token":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
  TOTAL=$((TOTAL + 1))
  if [ -n "$SESSION_TOKEN" ]; then
    PASS=$((PASS + 1))
    green "  PASS: User login (session token received)"
  else
    FAIL=$((FAIL + 1))
    red "  FAIL: User login"
    echo "    Response: ${LOGIN_RESP:0:200}"
  fi

  # Session token works (user is org_admin, has management access)
  if [ -n "$SESSION_TOKEN" ]; then
    check "Session token can access /v1/models" "200" -H "Authorization: Bearer $SESSION_TOKEN" "$BASE/v1/models"
    check "Session token org_admin can list users" "200" -H "Authorization: Bearer $SESSION_TOKEN" "$BASE/v1/management/users"
  fi

  # Deactivate user (cascade deactivates keys)
  check "Deactivate user (cascade)" "204" -X DELETE -H "Authorization: Bearer $KEY" "$BASE/v1/management/users/$USER_ID"

  # Deactivated session should fail
  if [ -n "$SESSION_TOKEN" ]; then
    check "Deactivated session -> expired/deactivated" "403" -H "Authorization: Bearer $SESSION_TOKEN" "$BASE/v1/models"
  fi
fi

# ---- Management: Pricing ----
bold "--- Pricing ---"
check_json_field "List pricing" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/pricing"

SET_PRICE=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"input_cost_per_token":0.00001,"output_cost_per_token":0.00003}' \
  "$BASE/v1/management/pricing/openai/gpt-4o")
TOTAL=$((TOTAL + 1))
if [ "$SET_PRICE" = "200" ]; then
  PASS=$((PASS + 1))
  green "  PASS: Set custom pricing"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: Set custom pricing (HTTP $SET_PRICE)"
fi

check "Delete custom pricing" "204" -X DELETE -H "Authorization: Bearer $KEY" "$BASE/v1/management/pricing/openai/gpt-4o"

# ---- Management: Usage ----
bold "--- Usage ---"
check_json_field "Get usage" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/usage"
check_json_field "Get daily usage" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/usage/daily"

# ---- Config ----
bold "--- Config ---"
check_json_field "GET /api/config" "200" '"model_list"' -H "Authorization: Bearer $KEY" "$BASE/api/config"

# Update settings
PUT_CFG_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"routing":{"strategy":"round-robin","retries":3},"rate_limiting":{"default_rpm":120}}' \
  "$BASE/api/config")
TOTAL=$((TOTAL + 1))
if [ "$PUT_CFG_CODE" = "200" ]; then
  PASS=$((PASS + 1))
  green "  PASS: PUT /api/config"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: PUT /api/config (HTTP $PUT_CFG_CODE)"
fi

# ---- Callbacks ----
bold "--- Callbacks ---"
check_json_field "List callbacks" "200" '"data"' -H "Authorization: Bearer $KEY" "$BASE/v1/management/callbacks"
check_json_field "Test callbacks" "200" '"results"' -X POST -H "Authorization: Bearer $KEY" "$BASE/v1/management/callbacks/test"

# ---- CORS ----
bold "--- CORS ---"
CORS_RESP=$(curl -s -D - -o /dev/null -X OPTIONS \
  -H "Origin: http://localhost:3000" \
  -H "Access-Control-Request-Method: GET" \
  "$BASE/v1/models" 2>&1)
TOTAL=$((TOTAL + 1))
if echo "$CORS_RESP" | grep -qi "access-control-allow-origin"; then
  PASS=$((PASS + 1))
  green "  PASS: CORS headers present"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: CORS headers missing"
fi

# ---- UI ----
bold "--- UI Reachability ---"
UI_BASE="${3:-http://localhost:3000}"
UI_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$UI_BASE" 2>/dev/null || echo "000")
TOTAL=$((TOTAL + 1))
if [ "$UI_CODE" = "200" ]; then
  PASS=$((PASS + 1))
  green "  PASS: UI index (HTTP $UI_CODE)"
else
  FAIL=$((FAIL + 1))
  red "  FAIL: UI index (HTTP $UI_CODE) -- UI may not be running"
fi

# ---- Summary ----
echo ""
bold "=== Results ==="
echo "Total: $TOTAL  |  Passed: $PASS  |  Failed: $FAIL"
echo ""

if [ "$FAIL" -gt 0 ]; then
  red "SOME TESTS FAILED"
  exit 1
else
  green "ALL TESTS PASSED"
  exit 0
fi
