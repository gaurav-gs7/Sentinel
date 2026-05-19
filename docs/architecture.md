# Sentinel Architecture

Sentinel is a Kubernetes reliability platform with a Harbor-style onboarding path and a Helios-style workflow execution substrate. Sentinel owns reliability policy and Kubernetes operations; workflow execution owns ordered steps, retries, state transitions, and audit events.

## Planes

Reliability plane:

- Sentinel API server
- Sentinel RolloutGuard controller/operator
- Service registry
- Production-readiness engine
- SLO/error-budget engine
- Rollout guard
- Incident engine
- RCA timeline and postmortem generator

Workflow execution plane:

- Helios-style workflow engine
- Workflow run state: pending, running, succeeded, failed
- Ordered workflow steps with retry metadata
- Idempotency keys for safely repeatable operations
- Audit-style workflow events exposed through `/api/v1/workflows`

Delivery plane:

- Generated service scaffold
- GitHub Actions workflow
- Kustomize base and overlays
- Argo CD application template
- Sentinel CRD-style resources: `SentinelService`, `SLOPolicy`, `RolloutGuard`, `Incident`
- kind-based local deployment runner

Observability plane:

- Prometheus scrape annotations
- ServiceMonitor template
- PrometheusRule and burn-rate alert templates
- Prometheus query-backed SLO evaluation for request rate, 5xx error rate, and p95 latency
- Grafana dashboard JSON
- OpenTelemetry Collector and Jaeger templates
- App metrics for request rate, error rate, p95/p99 latency, and database latency

## Request Flow

1. Developer runs `sentinel service init payments-api`.
2. CLI sends service metadata to `POST /api/v1/services`.
3. API validates metadata and production guardrails.
4. A workflow run starts for service onboarding.
5. Workflow steps store the service, record the initial deployment, and render the golden-path scaffold.
6. Template generator writes service code, Kubernetes manifests, CI/CD, SLO, rollout, observability, and runbook files.
7. Developer runs `sentinel check payments-api`.
8. Readiness evaluation runs as a workflow and returns production-readiness score, recommendations, and workflow evidence.
9. Local CI/CD runner builds images, deploys stable and canary revisions to kind, and verifies runtime health.
10. Health gate runs as a workflow that evaluates canary latency/error signals and records rollback intent if the policy breaches.
11. If the gate fails, the runner executes `kubectl rollout undo`.
12. Incident ingestion runs as a workflow that evaluates SLO impact, enriches service ownership, generates a timeline, and stores the incident record durably.

## Controller Reconciliation

The `sentinel-controller` process is the Kubernetes reconciliation layer. It watches `RolloutGuard` resources, resolves the referenced Deployment, evaluates live rollout state, updates `status.conditions`, emits Kubernetes Events, and restores the previous owned ReplicaSet template when a guarded rollout is blocked and rollback is enabled.

This keeps responsibilities separated:

- Sentinel API and Helios-style workflows execute explicit reliability operations and expose audit evidence.
- The controller continuously reconciles Kubernetes desired state and actual state.
- CRDs remain the cluster-native contract between service teams, GitOps, and Sentinel automation.

## Workflow Boundaries

Sentinel does not run a second standalone control plane next to Helios. Instead, Sentinel embeds Helios-style orchestration where reliability operations need durable execution semantics:

- `sentinel.service.onboarding`: validates and creates service metadata, records initial deployment, renders templates.
- `sentinel.readiness.check`: evaluates production-readiness policy.
- `sentinel.slo.evaluate`: evaluates SLO/error-budget state and deployment gate.
- `sentinel.rollout.start`: checks deployment eligibility and records rollout start.
- `sentinel.rollout.health-gate`: evaluates canary metrics and records rollback intent on breach.
- `sentinel.rollout.rollback`: records rollback intent for GitOps/Kubernetes execution.
- `sentinel.incident.enrichment`: correlates alerts with service metadata, SLO impact, RCA timeline, and postmortem context.

## Durable State

With PostgreSQL enabled, Sentinel persists:

- service catalog and deployment history
- workflow runs, steps, events, and idempotency keys
- incident records, enriched SLO signals, and RCA timeline events

Workflow idempotency lookup is durable, so repeated operations with the same idempotency key can be recognized after an API restart. Service onboarding uses a deterministic key derived from the normalized service spec.

## Local-First Production Design

Sentinel avoids paid services. It runs locally with Go, PostgreSQL through Docker Compose, kind, kubectl, Kustomize, and optional Trivy. The architecture still maps cleanly to production platforms because the generated contracts are Kubernetes-native and GitOps-friendly.
