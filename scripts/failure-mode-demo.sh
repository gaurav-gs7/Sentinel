#!/usr/bin/env bash
set -Eeuo pipefail

SERVICE_NAME="${SERVICE_NAME:-failure-api}"
SENTINEL_API_TOKEN="${SENTINEL_API_TOKEN:-local-dev-token}"
SENTINEL_PORT="${SENTINEL_PORT:-18081}"
SENTINEL_ADDR="${SENTINEL_ADDR:-127.0.0.1:$SENTINEL_PORT}"
SENTINEL_API_URL="${SENTINEL_API_URL:-http://127.0.0.1:$SENTINEL_PORT}"
ARTIFACT_ROOT="${ARTIFACT_ROOT:-artifacts/failure-modes}"
RUN_ID="${RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
PROMETHEUS_UNAVAILABLE_URL="${PROMETHEUS_UNAVAILABLE_URL:-http://127.0.0.1:1}"

export GOCACHE="${GOCACHE:-$(pwd)/.gocache}"
export GOMODCACHE="${GOMODCACHE:-$(pwd)/.gomodcache}"

run_dir="$ARTIFACT_ROOT/$RUN_ID"
log_file="$run_dir/failure-mode-demo.log"
summary_file="$run_dir/summary.json"
results_file="$run_dir/results.jsonl"
api_pid=""

mkdir -p "$run_dir"

log() {
  printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" | tee -a "$log_file"
}

record() {
  local scenario="$1"
  local evidence="$2"
  printf '{"scenario":"%s","status":"passed","evidence":"%s"}\n' "$scenario" "$evidence" >>"$results_file"
}

write_summary() {
  local status="$1"
  local detail="$2"
  cat >"$summary_file" <<EOF
{
  "status": "$status",
  "detail": "$detail",
  "runId": "$RUN_ID",
  "service": "$SERVICE_NAME",
  "artifacts": "$run_dir"
}
EOF
}

cleanup() {
  if [[ -n "$api_pid" ]]; then
    kill "$api_pid" >/dev/null 2>&1 || true
    wait "$api_pid" >/dev/null 2>&1 || true
  fi
}

on_error() {
  local line="$1"
  write_summary "failed" "failure-mode demo failed near line $line"
  log "failure-mode demo failed near line $line"
}

trap 'on_error $LINENO' ERR
trap cleanup EXIT

wait_for_url() {
  local url="$1"
  local tries="${2:-60}"
  for _ in $(seq 1 "$tries"); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  log "timed out waiting for $url"
  return 1
}

api() {
  local method="$1"
  local path="$2"
  local body="$3"
  local output="$4"
  local expected="$5"
  local status
  if [[ -n "$body" ]]; then
    status="$(curl -sS -o "$output" -w "%{http_code}" \
      -X "$method" \
      -H "Accept: application/json" \
      -H "Content-Type: application/json" \
      -H "X-Sentinel-Token: $SENTINEL_API_TOKEN" \
      --data "$body" \
      "$SENTINEL_API_URL$path")"
  else
    status="$(curl -sS -o "$output" -w "%{http_code}" \
      -X "$method" \
      -H "Accept: application/json" \
      -H "X-Sentinel-Token: $SENTINEL_API_TOKEN" \
      "$SENTINEL_API_URL$path")"
  fi
  if [[ "$status" != "$expected" ]]; then
    log "expected $method $path to return $expected, got $status"
    sed -n '1,160p' "$output" | tee -a "$log_file"
    return 1
  fi
}

assert_contains() {
  local file="$1"
  local pattern="$2"
  local message="$3"
  if ! grep -Eq "$pattern" "$file"; then
    log "$message"
    sed -n '1,160p' "$file" | tee -a "$log_file"
    return 1
  fi
}

start_api() {
  log "starting Sentinel API with unavailable Prometheus URL to prove fail-closed SLO behavior"
  env \
    GOCACHE="$GOCACHE" \
    GOMODCACHE="$GOMODCACHE" \
    SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" \
    SENTINEL_ADDR="$SENTINEL_ADDR" \
    SENTINEL_PROMETHEUS_URL="$PROMETHEUS_UNAVAILABLE_URL" \
    go run ./api/cmd/sentinel-api >>"$log_file" 2>&1 &
  api_pid="$!"
  for _ in $(seq 1 60); do
    if curl -fsS "$SENTINEL_API_URL/readyz" >/dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "$api_pid" >/dev/null 2>&1; then
      log "Sentinel API exited before becoming ready"
      return 1
    fi
    sleep 1
  done
  log "timed out waiting for $SENTINEL_API_URL/readyz"
  return 1
}

postgres_unavailable_fails_predictably() {
  local pg_log="$run_dir/postgres-unavailable.log"
  log "proving PostgreSQL unavailable fails predictably during startup"
  env \
    GOCACHE="$GOCACHE" \
    GOMODCACHE="$GOMODCACHE" \
    SENTINEL_ADDR="127.0.0.1:$((SENTINEL_PORT + 1))" \
    DATABASE_URL="postgres://sentinel:sentinel@127.0.0.1:1/sentinel?sslmode=disable" \
    go run ./api/cmd/sentinel-api >"$pg_log" 2>&1 &
  local pg_pid="$!"
  local exited="false"
  local exit_code="0"
  for _ in $(seq 1 30); do
    if ! kill -0 "$pg_pid" >/dev/null 2>&1; then
      if wait "$pg_pid"; then
        exit_code="0"
      else
        exit_code="$?"
      fi
      exited="true"
      break
    fi
    sleep 1
  done
  if [[ "$exited" != "true" ]]; then
    kill "$pg_pid" >/dev/null 2>&1 || true
    wait "$pg_pid" >/dev/null 2>&1 || true
    log "PostgreSQL unavailable process did not exit"
    return 1
  fi
  if [[ "$exit_code" == "0" ]]; then
    log "PostgreSQL unavailable process exited successfully, expected failure"
    return 1
  fi
  assert_contains "$pg_log" "open catalog store|connect|connection refused" "expected startup log to mention catalog/PostgreSQL connection failure"
  record "postgresql-unavailable-fails-predictably" "$pg_log"
}

invalid_metadata_rejected() {
  local output="$run_dir/invalid-service-metadata.json"
  log "proving invalid service metadata is rejected"
  api POST /api/v1/services '{"name":"Payments_API","team":"Platform","owner":"gaurav","language":"ruby","environment":"production","slo":"99.9","deploymentStrategy":"canary"}' "$output" 422
  assert_contains "$output" "policy validation failed|service-name|language|environment" "expected validation findings"
  record "invalid-service-metadata-rejected" "$output"
}

duplicate_onboarding_idempotent() {
  local first="$run_dir/onboard-first.json"
  local second="$run_dir/onboard-duplicate.json"
  local body
  body="{\"name\":\"$SERVICE_NAME\",\"team\":\"platform\",\"owner\":\"gaurav\",\"tier\":\"critical\",\"language\":\"go\",\"environment\":\"staging\",\"slo\":\"99.9\",\"deploymentStrategy\":\"canary\",\"pager\":\"platform-oncall\"}"
  log "proving duplicate onboarding is idempotent"
  api POST /api/v1/services "$body" "$first" 201
  api POST /api/v1/services "$body" "$second" 200
  assert_contains "$second" '"idempotent"[[:space:]]*:[[:space:]]*true' "expected duplicate onboarding to return idempotent true"
  record "duplicate-onboarding-idempotent" "$second"
}

low_traffic_waits_for_more_samples() {
  local output="$run_dir/low-traffic-health-gate.json"
  log "proving low traffic waits for more samples instead of rolling back"
  api POST "/api/v1/services/$SERVICE_NAME/health-gate" '{"p99LatencyMs":900,"errorRate":9.5,"successCount":10}' "$output" 200
  assert_contains "$output" '"action"[[:space:]]*:[[:space:]]*"continue"' "expected low traffic decision to continue"
  assert_contains "$output" 'waiting for more samples' "expected low traffic reason to wait for samples"
  record "low-traffic-waits-for-more-samples" "$output"
}

bad_canary_rolls_back_and_records_evidence() {
  local output="$run_dir/bad-canary-health-gate.json"
  local deployments="$run_dir/deployments-after-rollback.json"
  local rollback="$run_dir/rollback-evidence.json"
  log "proving a bad canary records rollback intent"
  api POST "/api/v1/services/$SERVICE_NAME/health-gate" '{"p99LatencyMs":450,"errorRate":0.2,"successCount":100}' "$output" 202
  assert_contains "$output" '"action"[[:space:]]*:[[:space:]]*"rollback"' "expected bad canary to request rollback"
  assert_contains "$output" '"rollbackDeployment"[[:space:]]*:[[:space:]]*\{' "expected rollback deployment evidence in health-gate response"
  api GET "/api/v1/services/$SERVICE_NAME/deployments" "" "$deployments" 200
  assert_contains "$deployments" 'rollback-triggered' "expected rollback deployment to be recorded"
  cp "$output" "$rollback"
  record "bad-canary-rolls-back" "$output"
  record "rollback-evidence-recorded" "$rollback"
}

prometheus_unavailable_fails_closed() {
  local output="$run_dir/prometheus-unavailable-slo.json"
  log "proving Prometheus unavailable causes safe degraded fail-closed behavior"
  api GET "/api/v1/services/$SERVICE_NAME/slos" "" "$output" 200
  assert_contains "$output" '"deploymentGate"[[:space:]]*:[[:space:]]*"blocked"' "expected Prometheus outage to block deployment gate"
  assert_contains "$output" 'Prometheus unavailable' "expected Prometheus outage reason"
  record "prometheus-unavailable-fails-closed" "$output"
}

main() {
  : >"$results_file"
  log "starting failure-mode demo; artifacts will be written to $run_dir"
  postgres_unavailable_fails_predictably
  start_api
  invalid_metadata_rejected
  duplicate_onboarding_idempotent
  low_traffic_waits_for_more_samples
  bad_canary_rolls_back_and_records_evidence
  prometheus_unavailable_fails_closed
  write_summary "passed" "all failure-mode scenarios passed"
  log "failure-mode demo passed; evidence written to $run_dir"
}

main "$@"
