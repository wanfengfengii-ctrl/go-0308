#!/usr/bin/env bash
set -euo pipefail

# Smoke test: builds the Go service, starts it against a temporary database,
# drives a real end-to-end workflow over HTTP, and verifies persistence and the
# frontend. It makes no external network calls and cleans up every process and
# temporary file it creates.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

TMP_DIR="$(mktemp -d)"
SERVER_PID=""
PORT="18087"
BASE="http://127.0.0.1:${PORT}"

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# assert_contains checks that a captured response contains a substring.
assert_contains() {
  local haystack="$1" needle="$2" msg="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "FAIL: $msg" >&2
    echo "  expected to find: $needle" >&2
    echo "  in response: $haystack" >&2
    exit 1
  fi
}

echo "==> building service"
go build -o "$TMP_DIR/potable-water-pipeline" ./cmd/server

echo "==> starting service on ${BASE}"
DB_PATH="$TMP_DIR/smoke.db" STATIC_DIR="$ROOT/web/dist" ADDR="127.0.0.1:${PORT}" \
  "$TMP_DIR/potable-water-pipeline" &
SERVER_PID=$!

# Wait for the health endpoint to come up (bounded).
health=""
for _ in $(seq 1 50); do
  if health="$(curl -s --max-time 2 "$BASE/health" 2>/dev/null)"; then
    if [[ "$health" == *'"status":"ok"'* ]]; then
      break
    fi
  fi
  sleep 0.1
done
assert_contains "$health" '"status":"ok"' "health endpoint did not report ok"

echo "==> creating a job"
job_body='{
  "topology": {
    "nodes": [
      {"id":"n1","is_boundary":true},
      {"id":"n2"},
      {"id":"n3","is_boundary":true}
    ],
    "sections": [
      {"id":"s1","from":"n1","to":"n2","diameter_mm":100,"length_m":100},
      {"id":"s2","from":"n2","to":"n3","diameter_mm":100,"length_m":100,"is_blind_end":true}
    ],
    "valves": [
      {"id":"v1","section_id":"s1","closed":true},
      {"id":"v2","section_id":"s2","closed":true}
    ],
    "outlets": [{"id":"o1","section_id":"s2"}],
    "injections": [{"id":"inj1","section_id":"s1"}],
    "sampling": [{"id":"sp1","section_id":"s2","order":1}]
  },
  "targets": {
    "min_flow": {"value":500,"scale":0},
    "max_turbidity": {"value":5,"scale":0},
    "min_window_count": 2,
    "min_initial_conc": {"value":25,"scale":0},
    "min_terminal_conc": {"value":10,"scale":0},
    "min_ct": {"value":960,"scale":0},
    "contact_duration": 10,
    "turnover_target": 20,
    "turnover_scale": 1
  },
  "rule_version": 1
}'

create_resp="$(curl -s --max-time 5 -X POST "$BASE/api/jobs?id=smoke-job" -H 'Content-Type: application/json' -d "$job_body")"
assert_contains "$create_resp" '"id":"smoke-job"' "job creation did not return the job id"

echo "==> verifying persisted job listing"
list_resp="$(curl -s --max-time 5 "$BASE/api/jobs")"
assert_contains "$list_resp" '"id":"smoke-job"' "job listing did not include the created job"
assert_contains "$list_resp" '"stage":"isolation_verify"' "job did not start at isolation verification"

echo "==> verifying frontend is served"
page="$(curl -s --max-time 5 "$BASE/")"
assert_contains "$page" '饮用水管段' "frontend page did not render the expected title"

echo "==> smoke test passed"
