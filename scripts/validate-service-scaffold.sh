#!/usr/bin/env bash
set -euo pipefail

service_dir="${1:?usage: scripts/validate-service-scaffold.sh <generated-service-dir>}"

failures=()

require_file() {
  local path="$1"
  if [[ ! -f "$service_dir/$path" ]]; then
    failures+=("missing file: $path")
  fi
}

require_grep() {
  local pattern="$1"
  local path="$2"
  local message="$3"
  if ! grep -Eq "$pattern" "$service_dir/$path"; then
    failures+=("$message in $path")
  fi
}

require_file "Dockerfile"
require_file "sentinel-service.yaml"
require_file "slo.yaml"
require_file "rollout.yaml"
require_file ".github/workflows/ci.yml"
require_file "k8s/base/deployment.yaml"
require_file "k8s/base/serviceaccount.yaml"
require_file "k8s/base/hpa.yaml"
require_file "k8s/base/pdb.yaml"
require_file "k8s/overlays/staging/kustomization.yaml"
require_file "observability/dashboard.json"
require_file "observability/alerts.yaml"
require_file "observability/prometheus-rules.yaml"
require_file "observability/servicemonitor.yaml"
require_file "observability/otel-collector.yaml"
require_file "observability/jaeger.yaml"
require_file "runbooks/incident-runbook.md"
require_file "infra/argocd/application.yaml"
require_file "infra/terraform/main.tf"

require_grep "readinessProbe:" "k8s/base/deployment.yaml" "readiness probe is required"
require_grep "livenessProbe:" "k8s/base/deployment.yaml" "liveness probe is required"
require_grep "requests:" "k8s/base/deployment.yaml" "resource requests are required"
require_grep "limits:" "k8s/base/deployment.yaml" "resource limits are required"
require_grep "PodDisruptionBudget" "k8s/base/pdb.yaml" "PDB is required"
require_grep "runAsNonRoot: true" "k8s/base/deployment.yaml" "containers must run as non-root"
require_grep "allowPrivilegeEscalation: false" "k8s/base/deployment.yaml" "privilege escalation must be disabled"
require_grep "readOnlyRootFilesystem: true" "k8s/base/deployment.yaml" "root filesystem must be read-only"
require_grep 'drop: \["ALL"\]' "k8s/base/deployment.yaml" "Linux capabilities must be dropped"
require_grep "automountServiceAccountToken: false" "k8s/base/serviceaccount.yaml" "service account token automount must be disabled"
require_grep "prometheus.io/scrape" "k8s/base/deployment.yaml" "Prometheus scrape annotations are required"
require_grep "Trivy|trivy" ".github/workflows/ci.yml" "CI must include image scanning"
require_grep "kubectl kustomize" ".github/workflows/ci.yml" "CI must render Kubernetes manifests"
require_grep "Application" "infra/argocd/application.yaml" "Argo CD application is required"
require_grep "high_p99_latency|high_error_rate" "observability/alerts.yaml" "SLO alerts are required"
require_grep "high_p95_latency" "observability/alerts.yaml" "p95 latency alert is required"
require_grep "high_database_latency" "observability/alerts.yaml" "database latency alert is required"
require_grep "fast_burn_rate" "observability/prometheus-rules.yaml" "burn-rate alert is required"
require_grep "ServiceMonitor" "observability/servicemonitor.yaml" "Prometheus ServiceMonitor is required"
require_grep "SentinelService" "sentinel-service.yaml" "SentinelService CR is required"
require_grep "SLOPolicy" "slo.yaml" "SLOPolicy CR is required"
require_grep "RolloutGuard" "rollout.yaml" "RolloutGuard CR is required"
require_grep "P95 Latency" "observability/dashboard.json" "p95 latency dashboard panel is required"
require_grep "Database P95 Latency" "observability/dashboard.json" "database latency dashboard panel is required"
require_grep "otel-collector" "observability/otel-collector.yaml" "OpenTelemetry collector config is required"
require_grep "jaeger" "observability/jaeger.yaml" "Jaeger tracing config is required"

if (( ${#failures[@]} > 0 )); then
  printf 'service scaffold validation failed:\n' >&2
  for failure in "${failures[@]}"; do
    printf '  - %s\n' "$failure" >&2
  done
  exit 1
fi

printf 'service scaffold validation passed for %s\n' "$service_dir"
