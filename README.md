# Sentinel

Sentinel is a production-style Kubernetes reliability platform for SLOs, progressive delivery, production readiness, and incident automation. Reliability actions are executed through a Helios-style workflow engine instead of a bespoke standalone control loop.

It keeps the useful Harbor-style golden path for service onboarding, but the product focus is stronger: developers can ship services quickly, while platform/SRE teams get reliability controls before and during production rollout.

## What Sentinel Does

- Registers services with owner, tier, pager, repository, runbook, dashboard, SLO, deployment strategy, and rollback policy metadata.
- Generates production-ready service scaffolds with Kubernetes manifests, GitHub Actions, Kustomize overlays, Terraform, Argo CD, Grafana, Alertmanager, OpenTelemetry, Jaeger, SLO policy, rollout guard, and runbook files.
- Scores services for production readiness.
- Evaluates SLO/error-budget status and deployment eligibility.
- Gates canary rollouts with latency and error-rate health checks.
- Reconciles `RolloutGuard` resources with a Kubernetes controller that updates status, emits Events, and restores the previous ReplicaSet template when guarded rollouts are blocked.
- Records automated rollback decisions through workflow-backed rollout steps.
- Converts simulated Alertmanager signals into incidents with enriched SLO state, timeline, and postmortem draft.
- Exposes workflow runs, steps, retry state, and audit-style events for onboarding, readiness, SLO checks, rollouts, rollback, and incidents.
- Runs locally on a MacBook with Go, Docker, kind, kubectl, and optional Trivy.

## Helios Integration

Sentinel owns the Kubernetes reliability domain. The Helios-style workflow engine owns orchestration semantics: ordered steps, retries, run state, idempotency keys, and execution events. This keeps Sentinel distinct from Helios while reusing the strongest control-plane idea from Helios as the execution substrate.

Workflow-backed operations include:

- Service onboarding and scaffold generation.
- Production-readiness and SLO/error-budget evaluation.
- Rollout start, pause, rollback, and health-gate rollback.
- Alertmanager-style incident enrichment, RCA timeline generation, and incident resolution.

## Quick Start

Start the API:

```bash
make run-api
```

In another terminal:

```bash
export SENTINEL_API_TOKEN=local-dev-token

go run ./cli/cmd/sentinel service init payments-api \
  --owner payments-platform \
  --team platform \
  --tier critical \
  --language go \
  --slo-availability 99.9 \
  --slo-latency-p95 300ms \
  --deployment canary \
  --pager payments-oncall
```

Generated service assets appear under:

```text
generated/services/payments-api
```

## CLI Demo

```bash
go run ./cli/cmd/sentinel check payments-api
go run ./cli/cmd/sentinel score payments-api
go run ./cli/cmd/sentinel slo status payments-api
go run ./cli/cmd/sentinel rollout status payments-api
go run ./cli/cmd/sentinel health-gate --name payments-api --p99-latency-ms 450 --error-rate 0.2 --success-count 100
go run ./cli/cmd/sentinel incident create --service payments-api --alert HighErrorRate --error-rate 8.2 --latency-p95-ms 850
go run ./cli/cmd/sentinel incident list
go run ./cli/cmd/sentinel workflows list
```

## Full Local CI/CD Demo

```bash
make demo-full
```

This runs a production-style local pipeline:

1. Starts or reuses the Sentinel API.
2. Onboards `payments-api`.
3. Validates generated production-readiness guardrails.
4. Runs service tests.
5. Renders Kustomize manifests.
6. Builds stable and canary Docker images.
7. Runs Trivy when installed.
8. Creates or reuses a kind cluster.
9. Deploys the stable revision.
10. Deploys the canary revision.
11. Verifies `/readyz` and `/metrics`.
12. Sends a bad canary signal to Sentinel.
13. Records a rollback decision.
14. Runs `kubectl rollout undo`.
15. Creates an incident.
16. Generates timeline and postmortem output.
17. Lists Helios-style workflow runs for audit evidence.
18. Writes evidence to `artifacts/local-ci-cd/`.

## Kubernetes Controller

Sentinel includes a controller/operator for `RolloutGuard` resources:

```bash
go run ./controller/cmd/sentinel-controller --kubeconfig "$HOME/.kube/config"
```

In-cluster manifests live under:

```bash
kubectl apply -k deploy/crds
kubectl apply -k deploy/controller
```

The controller watches `RolloutGuard`, evaluates the referenced Deployment, patches `status.conditions`, creates Kubernetes Events, and triggers rollback by restoring the previous owned ReplicaSet template when the rollout is blocked and rollback is enabled. See `docs/controller-operator.md`.

## Generated Service Files

`sentinel service init` generates:

```text
generated/services/payments-api/
  sentinel-service.yaml
  slo.yaml
  rollout.yaml
  Dockerfile
  Makefile
  .github/workflows/ci.yml
  k8s/base/deployment.yaml
  k8s/base/service.yaml
  k8s/base/hpa.yaml
  k8s/base/pdb.yaml
  observability/dashboard.json
  observability/alerts.yaml
  observability/prometheus-rules.yaml
  observability/servicemonitor.yaml
  observability/otel-collector.yaml
  observability/jaeger.yaml
  runbooks/incident-runbook.md
  infra/argocd/application.yaml
  infra/terraform/main.tf
```

## API Surface

- `POST /api/v1/services`
- `GET /api/v1/services`
- `GET /api/v1/services/{name}`
- `GET /api/v1/services/{name}/readiness`
- `GET /api/v1/services/{name}/score`
- `GET /api/v1/services/{name}/slos`
- `GET /api/v1/services/{name}/error-budget`
- `POST /api/v1/services/{name}/health-gate`
- `POST /api/v1/services/{name}/rollback`
- `POST /api/v1/services/{name}/rollouts`
- `GET /api/v1/services/{name}/rollouts/{id}`
- `POST /api/v1/services/{name}/rollouts/{id}/pause`
- `POST /api/v1/services/{name}/rollouts/{id}/rollback`
- `POST /api/v1/incidents`
- `GET /api/v1/incidents`
- `GET /api/v1/incidents/{id}`
- `GET /api/v1/incidents/{id}/timeline`
- `GET /api/v1/incidents/{id}/postmortem`
- `GET /api/v1/workflows`
- `GET /api/v1/workflows/{id}`

## PostgreSQL Mode

```bash
docker compose up --build
```

Without `DATABASE_URL`, Sentinel uses an in-memory store for fast laptop-local development. With `DATABASE_URL`, Sentinel persists the service catalog, deployments, reliability metadata, and workflow run tables in PostgreSQL.

## Repository Layout

```text
api/          Go API, catalog, workflow engine, readiness, SLO, rollout, incident logic
cli/          Go CLI for service onboarding and reliability workflows
controller/   Kubernetes RolloutGuard controller/operator
templates/    Service, CI/CD, GitOps, observability, SLO, rollout, and runbook templates
deploy/crds/  SentinelService, SLOPolicy, RolloutGuard, and Incident CRDs
infra/        Terraform, Argo CD, and monitoring defaults
scripts/      Local demo and CI/CD runner
docs/         Architecture and operator documentation
```

## Resume Positioning

Sentinel — Kubernetes Reliability Platform with Helios-Style Workflow Orchestration

- Built a Go-based Kubernetes reliability platform that standardized service onboarding using CRDs, CLI workflows, GitOps templates, SLO policies, runbooks, dashboards, and production-readiness checks.
- Integrated a Helios-style workflow engine for onboarding, SLO checks, rollout gates, rollback decisions, incident enrichment, ordered execution steps, retry metadata, and audit events.
- Implemented a production-readiness engine that validates ownership, tier, pager, SLOs, rollout policy, rollback readiness, generated Kubernetes guardrails, observability defaults, and runbook coverage.
- Developed SLO/error-budget tracking to evaluate availability, p95 latency, error rate, burn rate, and deployment eligibility for Kubernetes services.
- Integrated local GitHub Actions-style CI/CD, Kustomize, kind, Prometheus/Grafana templates, Alertmanager-style incident ingestion, automated rollback evidence, and RCA timeline/postmortem generation.
