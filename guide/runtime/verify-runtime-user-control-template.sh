#!/usr/bin/env bash
# Runtime control verification template for sing-box /runtime API.
# This script checks:
# 1) runtime/users upsert
# 2) runtime/disconnect by user_id
# 3) runtime/policy with wildcard principal (user_id:*)

set -euo pipefail

RUNTIME_URL="${RUNTIME_URL:-http://127.0.0.1:9090/runtime}"
INBOUND_TAG="${INBOUND_TAG:-vless-in}"
TEST_USER_ID="${TEST_USER_ID:-runtime-test-user}"
TEST_DEVICE_A="${TEST_DEVICE_A:-device-a}"
TEST_DEVICE_B="${TEST_DEVICE_B:-device-b}"
TEST_UUID_A="${TEST_UUID_A:-11111111-1111-1111-1111-111111111111}"
TEST_UUID_B="${TEST_UUID_B:-22222222-2222-2222-2222-222222222222}"

REQ_BASE="verify-$(date +%s)"
REV_BASE="$(date +%s)000"

echo "== runtime/users upsert =="
curl -fsS -X PUT "${RUNTIME_URL}/users" \
  -H "Content-Type: application/json" \
  -d "{
    \"revision\": ${REV_BASE},
    \"request_id\": \"${REQ_BASE}-users\",
    \"operations\": [
      {
        \"inbound\": \"${INBOUND_TAG}\",
        \"upsert\": [
          {\"principal\": \"${TEST_USER_ID}:${TEST_DEVICE_A}\", \"uuid\": \"${TEST_UUID_A}\", \"enabled\": true},
          {\"name\": \"${TEST_USER_ID}:${TEST_DEVICE_B}\", \"uuid\": \"${TEST_UUID_B}\", \"enabled\": true}
        ]
      }
    ]
  }" | jq .

echo "== runtime/policy wildcard (user_id:*) =="
curl -fsS -X PUT "${RUNTIME_URL}/policy" \
  -H "Content-Type: application/json" \
  -d "{
    \"revision\": $((REV_BASE + 1)),
    \"request_id\": \"${REQ_BASE}-policy\",
    \"replace\": false,
    \"policies\": [
      {\"principal\": \"${TEST_USER_ID}:*\", \"max_connections\": 2, \"up_bps\": 1048576, \"down_bps\": 2097152}
    ]
  }" | jq .

echo "== runtime/disconnect user_id (disconnect user_id + user_id:*) =="
curl -fsS -X POST "${RUNTIME_URL}/disconnect" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\": \"${TEST_USER_ID}\"
  }" | jq .

echo "== runtime/stats/snapshot =="
curl -fsS "${RUNTIME_URL}/stats/snapshot" | jq .

echo "Done."
