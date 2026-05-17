#!/usr/bin/env bash
set -Eeuo pipefail

SERVICE_NAME="${SERVICE_NAME:-payments-api}"
SERVICE_LANGUAGE="${SERVICE_LANGUAGE:-go}"
SERVICE_TEAM="${SERVICE_TEAM:-platform}"
SERVICE_OWNER="${SERVICE_OWNER:-gaurav}"
SERVICE_TIER="${SERVICE_TIER:-critical}"
SERVICE_PAGER="${SERVICE_PAGER:-payments-oncall}"
SERVICE_ENV="${SERVICE_ENV:-staging}"
SERVICE_SLO="${SERVICE_SLO:-99.9}"
SERVICE_LATENCY_P95="${SERVICE_LATENCY_P95:-300ms}"
SERVICE_STRATEGY="${SERVICE_STRATEGY:-canary}"
SENTINEL_API_TOKEN="${SENTINEL_API_TOKEN:-local-dev-token}"
SENTINEL_API_URL="${SENTINEL_API_URL:-http://127.0.0.1:8080}"
SENTINEL_ADDR="${SENTINEL_ADDR:-127.0.0.1:8080}"
CLUSTER_NAME="${CLUSTER_NAME:-sentinel-local}"
NAMESPACE="${NAMESPACE:-$SERVICE_TEAM}"
IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-ghcr.io/example/$SERVICE_NAME}"
ARTIFACT_ROOT="${ARTIFACT_ROOT:-artifacts/local-ci-cd}"
TRIVY_REQUIRED="${TRIVY_REQUIRED:-false}"
AUTO_ROLLBACK="${AUTO_ROLLBACK:-true}"
KEEP_CLUSTER="${KEEP_CLUSTER:-true}"
CLEAN_STALE_PODS="${CLEAN_STALE_PODS:-true}"
PORT_FORWARD_PORT="${PORT_FORWARD_PORT:-18080}"
RUN_ID="${RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
STABLE_IMAGE_TAG="${STABLE_IMAGE_TAG:-$RUN_ID-stable}"
CANARY_IMAGE_TAG="${CANARY_IMAGE_TAG:-$RUN_ID-canary}"
STABLE_IMAGE_NAME="${STABLE_IMAGE_NAME:-$IMAGE_REPOSITORY:$STABLE_IMAGE_TAG}"
CANARY_IMAGE_NAME="${CANARY_IMAGE_NAME:-$IMAGE_REPOSITORY:$CANARY_IMAGE_TAG}"
IMAGE_NAME="${IMAGE_NAME:-$CANARY_IMAGE_NAME}"

export SENTINEL_API_TOKEN SENTINEL_API_URL SENTINEL_ADDR
export GOCACHE="${GOCACHE:-$(pwd)/.gocache}"
export GOMODCACHE="${GOMODCACHE:-$(pwd)/.gomodcache}"

run_dir="$ARTIFACT_ROOT/$RUN_ID"
log_file="$run_dir/pipeline.log"
summary_file="$run_dir/summary.json"
rendered_file="$run_dir/rendered.yaml"
stable_rendered_file="$run_dir/rendered-stable.yaml"
canary_rendered_file="$run_dir/rendered-canary.yaml"
rollback_file="$run_dir/rollback.json"
incident_file="$run_dir/incident.json"
api_pid=""
port_forward_pid=""
original_context="$(kubectl config current-context 2>/dev/null || true)"

mkdir -p "$run_dir"

log() {
  printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" | tee -a "$log_file"
}

run() {
  log "+ $*"
  "$@" 2>&1 | tee -a "$log_file"
}

run_in_dir() {
  local dir="$1"
  shift
  log "+ (cd $dir && $*)"
  (cd "$dir" && "$@") 2>&1 | tee -a "$log_file"
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
  "environment": "$SERVICE_ENV",
  "namespace": "$NAMESPACE",
  "cluster": "$CLUSTER_NAME",
  "stableImage": "$STABLE_IMAGE_NAME",
  "canaryImage": "$CANARY_IMAGE_NAME",
  "artifacts": "$run_dir"
}
EOF
}

cleanup() {
  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$api_pid" ]]; then
    kill "$api_pid" >/dev/null 2>&1 || true
    wait "$api_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$original_context" ]]; then
    kubectl config use-context "$original_context" >/dev/null 2>&1 || true
  fi
}

on_error() {
  local line="$1"
  cleanup
  write_summary "failed" "pipeline failed near line $line"
  log "pipeline failed near line $line"
}

trap 'on_error $LINENO' ERR
trap cleanup EXIT

require_command() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    log "missing required command: $name"
    exit 1
  fi
}

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

preflight() {
  log "preflight checks"
  require_command go
  require_command docker
  require_command kind
  require_command kubectl
  require_command curl
  require_command perl
  run docker info
  if ! command -v trivy >/dev/null 2>&1; then
    if [[ "$TRIVY_REQUIRED" == "true" ]]; then
      log "trivy is required but not installed"
      exit 1
    fi
    log "trivy not found; image scan will be recorded as skipped"
  fi
}

ensure_sentinel_api() {
  if curl -fsS "$SENTINEL_API_URL/readyz" >/dev/null 2>&1; then
    log "Sentinel API already running at $SENTINEL_API_URL"
    return
  fi

  log "starting Sentinel API at $SENTINEL_ADDR"
  env \
    GOCACHE="$GOCACHE" \
    GOMODCACHE="$GOMODCACHE" \
    SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" \
    SENTINEL_ADDR="$SENTINEL_ADDR" \
    go run ./api/cmd/sentinel-api >>"$log_file" 2>&1 &
  api_pid="$!"
  wait_for_url "$SENTINEL_API_URL/readyz" 60
}

onboard_service() {
  log "onboarding $SERVICE_NAME through Sentinel"
  run env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" SENTINEL_API_URL="$SENTINEL_API_URL" \
    go run ./cli/cmd/sentinel service init "$SERVICE_NAME" \
      --language "$SERVICE_LANGUAGE" \
      --team "$SERVICE_TEAM" \
      --owner "$SERVICE_OWNER" \
      --tier "$SERVICE_TIER" \
      --pager "$SERVICE_PAGER" \
      --namespace "$NAMESPACE" \
      --env "$SERVICE_ENV" \
      --slo-availability "$SERVICE_SLO" \
      --slo-latency-p95 "$SERVICE_LATENCY_P95" \
      --deployment "$SERVICE_STRATEGY"
}

service_ci() {
  local service_dir="generated/services/$SERVICE_NAME"
  log "running local CI checks for $service_dir"
  run scripts/validate-service-scaffold.sh "$service_dir"

  if [[ "$SERVICE_LANGUAGE" == "go" ]]; then
    run_in_dir "$service_dir" env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" go test ./...
  elif [[ "$SERVICE_LANGUAGE" == "python" ]]; then
    run_in_dir "$service_dir" python3 -m compileall app
  else
    log "unsupported language for local CI: $SERVICE_LANGUAGE"
    exit 1
  fi

  run kubectl kustomize "$service_dir/k8s/overlays/$SERVICE_ENV"
  kubectl kustomize "$service_dir/k8s/overlays/$SERVICE_ENV" >"$rendered_file"
  cp "$rendered_file" "$stable_rendered_file"
  cp "$rendered_file" "$canary_rendered_file"
  perl -0pi -e "s#image: ghcr.io/example/$SERVICE_NAME:latest#image: $STABLE_IMAGE_NAME#g" "$stable_rendered_file"
  perl -0pi -e "s#image: ghcr.io/example/$SERVICE_NAME:latest#image: $CANARY_IMAGE_NAME#g" "$canary_rendered_file"
  log "pinned stable manifest image to $STABLE_IMAGE_NAME"
  log "pinned canary manifest image to $CANARY_IMAGE_NAME"
}

build_and_scan_image() {
  local service_dir="generated/services/$SERVICE_NAME"
  log "building stable and canary container images"
  run docker build -t "$STABLE_IMAGE_NAME" -t "$CANARY_IMAGE_NAME" "$service_dir"

  if command -v trivy >/dev/null 2>&1; then
    log "scanning canary image with Trivy"
    run trivy image --severity HIGH,CRITICAL --exit-code 1 --ignore-unfixed "$CANARY_IMAGE_NAME"
  else
    printf 'trivy scan skipped: trivy not installed\n' >"$run_dir/trivy-skipped.txt"
  fi
}

ensure_kind_cluster() {
  if kind get clusters | grep -qx "$CLUSTER_NAME"; then
    log "kind cluster $CLUSTER_NAME already exists"
    if kind export kubeconfig --name "$CLUSTER_NAME" 2>&1 | tee -a "$log_file"; then
      return
    fi
    log "kind cluster $CLUSTER_NAME is stale or unhealthy; recreating it"
    run kind delete cluster --name "$CLUSTER_NAME"
  fi

  log "creating kind cluster $CLUSTER_NAME"
  run kind create cluster --name "$CLUSTER_NAME" --wait 120s
}

deploy_to_kind() {
  log "loading image into kind"
  run kind load docker-image "$STABLE_IMAGE_NAME" --name "$CLUSTER_NAME"
  run kind load docker-image "$CANARY_IMAGE_NAME" --name "$CLUSTER_NAME"

  log "deploying stable rendered manifests"
  run kubectl config use-context "kind-$CLUSTER_NAME"
  run kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml
  kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - 2>&1 | tee -a "$log_file"
  if [[ "$CLEAN_STALE_PODS" == "true" ]]; then
    run kubectl -n "$NAMESPACE" delete pod -l "app.kubernetes.io/name=$SERVICE_NAME" --field-selector=status.phase!=Running --ignore-not-found=true
  fi
  run kubectl apply --dry-run=server -f "$stable_rendered_file"
  run kubectl apply -f "$stable_rendered_file"
  run kubectl -n "$NAMESPACE" rollout status "deployment/$SERVICE_NAME" --timeout=180s
  verify_deployment_image "$STABLE_IMAGE_NAME"

  log "deploying canary rendered manifests"
  run kubectl apply --dry-run=server -f "$canary_rendered_file"
  run kubectl apply -f "$canary_rendered_file"
  run kubectl -n "$NAMESPACE" rollout status "deployment/$SERVICE_NAME" --timeout=180s
  verify_deployment_image "$CANARY_IMAGE_NAME"
  run kubectl -n "$NAMESPACE" get "deployment/$SERVICE_NAME" "service/$SERVICE_NAME" "hpa/$SERVICE_NAME" -o wide
  run kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/name=$SERVICE_NAME,sentinel.dev/environment=$SERVICE_ENV" --field-selector=status.phase=Running -o wide
}

verify_deployment_image() {
  local expected="$1"
  local actual
  actual="$(kubectl -n "$NAMESPACE" get "deployment/$SERVICE_NAME" -o jsonpath='{.spec.template.spec.containers[0].image}')"
  log "deployment image is $actual"
  if [[ "$actual" != "$expected" ]]; then
    log "expected deployment image $expected but found $actual"
    exit 1
  fi
}

verify_runtime() {
  log "verifying service readiness and metrics through port-forward"
  kubectl -n "$NAMESPACE" port-forward "svc/$SERVICE_NAME" "$PORT_FORWARD_PORT:80" >>"$log_file" 2>&1 &
  port_forward_pid="$!"
  wait_for_url "http://127.0.0.1:$PORT_FORWARD_PORT/readyz" 60
  run curl -fsS "http://127.0.0.1:$PORT_FORWARD_PORT/readyz"
  run curl -fsS "http://127.0.0.1:$PORT_FORWARD_PORT/metrics"
}

sentinel_reliability_demo() {
  log "recording failed canary through Sentinel health gate"
  run env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" SENTINEL_API_URL="$SENTINEL_API_URL" \
    go run ./cli/cmd/sentinel health-gate \
      --name "$SERVICE_NAME" \
      --p99-latency-ms 450 \
      --error-rate 0.2 \
      --success-count 100

  if [[ "$AUTO_ROLLBACK" == "true" ]]; then
    automate_kubernetes_rollback
  fi

  run env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" SENTINEL_API_URL="$SENTINEL_API_URL" \
    go run ./cli/cmd/sentinel check "$SERVICE_NAME"
  run env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" SENTINEL_API_URL="$SENTINEL_API_URL" \
    go run ./cli/cmd/sentinel slo status "$SERVICE_NAME"
  run env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" SENTINEL_API_URL="$SENTINEL_API_URL" \
    go run ./cli/cmd/sentinel deployments --name "$SERVICE_NAME"
  simulate_incident
  run env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" SENTINEL_API_URL="$SENTINEL_API_URL" \
    go run ./cli/cmd/sentinel workflows list
  run curl -fsS "$SENTINEL_API_URL/metrics"
}

simulate_incident() {
  log "creating incident from simulated Alertmanager signal"
  env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" SENTINEL_API_URL="$SENTINEL_API_URL" \
    go run ./cli/cmd/sentinel incident create \
      --service "$SERVICE_NAME" \
      --alert HighErrorRate \
      --error-rate 8.2 \
      --latency-p95-ms 850 | tee "$incident_file" | tee -a "$log_file"
  local incident_id
  incident_id="$(perl -ne 'print "$1\n" if /\"id\": \"([^\"]+)\"/' "$incident_file" | head -n1)"
  if [[ -n "$incident_id" ]]; then
    run env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" SENTINEL_API_URL="$SENTINEL_API_URL" \
      go run ./cli/cmd/sentinel incident timeline "$incident_id"
    run env GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" SENTINEL_API_TOKEN="$SENTINEL_API_TOKEN" SENTINEL_API_URL="$SENTINEL_API_URL" \
      go run ./cli/cmd/sentinel incident postmortem "$incident_id"
  fi
}

automate_kubernetes_rollback() {
  log "automating Kubernetes rollback to previous stable ReplicaSet"
  run kubectl -n "$NAMESPACE" rollout history "deployment/$SERVICE_NAME"
  run kubectl -n "$NAMESPACE" rollout undo "deployment/$SERVICE_NAME"
  run kubectl -n "$NAMESPACE" rollout status "deployment/$SERVICE_NAME" --timeout=180s
  verify_deployment_image "$STABLE_IMAGE_NAME"
  run kubectl -n "$NAMESPACE" get "deployment/$SERVICE_NAME" -o wide
  cat >"$rollback_file" <<EOF
{
  "status": "rolled_back",
  "service": "$SERVICE_NAME",
  "namespace": "$NAMESPACE",
  "fromImage": "$CANARY_IMAGE_NAME",
  "toImage": "$STABLE_IMAGE_NAME",
  "verifiedAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
  log "rollback evidence written to $rollback_file"
}

main() {
  log "starting local CI/CD pipeline for $SERVICE_NAME"
  preflight
  ensure_sentinel_api
  onboard_service
  service_ci
  build_and_scan_image
  ensure_kind_cluster
  deploy_to_kind
  verify_runtime
  sentinel_reliability_demo
  write_summary "passed" "local CI/CD pipeline completed"
  log "pipeline passed; evidence written to $run_dir"
  if [[ "$KEEP_CLUSTER" != "true" ]]; then
    run kind delete cluster --name "$CLUSTER_NAME"
  fi
}

main "$@"
