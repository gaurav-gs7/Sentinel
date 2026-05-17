#!/usr/bin/env bash
set -euo pipefail

export SENTINEL_API_TOKEN="${SENTINEL_API_TOKEN:-local-dev-token}"
export SENTINEL_ADDR="${SENTINEL_ADDR:-127.0.0.1:8080}"
export SENTINEL_API_URL="${SENTINEL_API_URL:-http://127.0.0.1:8080}"
export GOCACHE="${GOCACHE:-$(pwd)/.gocache}"
export GOMODCACHE="${GOMODCACHE:-$(pwd)/.gomodcache}"

go run ./api/cmd/sentinel-api &
api_pid=$!
trap 'kill "$api_pid" 2>/dev/null || true' EXIT

for _ in $(seq 1 30); do
  if curl -fsS "$SENTINEL_API_URL/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

go run ./cli/cmd/sentinel service init payments-api \
  --language go \
  --team platform \
  --owner payments-platform \
  --tier critical \
  --pager payments-oncall \
  --env staging \
  --slo-availability 99.9 \
  --slo-latency-p95 300ms \
  --deployment canary

go run ./cli/cmd/sentinel check payments-api
go run ./cli/cmd/sentinel slo status payments-api
go run ./cli/cmd/sentinel rollout status payments-api
go run ./cli/cmd/sentinel health-gate --name payments-api --p99-latency-ms 450 --error-rate 0.2 --success-count 100
go run ./cli/cmd/sentinel deployments --name payments-api
go run ./cli/cmd/sentinel incident create --service payments-api --alert HighErrorRate --error-rate 8.2 --latency-p95-ms 850
go run ./cli/cmd/sentinel incident list
go run ./cli/cmd/sentinel workflows list

curl -fsS "$SENTINEL_API_URL/metrics" | sed -n '1,20p'
