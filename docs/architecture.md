# Attesta Architecture

Attesta is a Kubernetes reliability platform with a Harbor-style onboarding path and an embedded Helios-style workflow runner. Attesta owns reliability policy and Kubernetes operations; the runner owns ordered in-process steps, retries, state transitions, and audit events.

## Planes

Reliability plane:

- Attesta API server
- Attesta RolloutGuard controller/operator
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
- Attesta CRD-style resources: `AttestaService`, `SLOPolicy`, `RolloutGuard`, `Incident`
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

1. Developer runs `attesta service init payments-api`.
2. CLI sends service metadata to `POST /api/v1/services`.
3. API validates metadata and production guardrails.
4. A workflow run starts for service onboarding.
5. Workflow steps store the service, record the initial deployment, and render the golden-path scaffold.
6. Template generator writes service code, Kubernetes manifests, CI/CD, SLO, rollout, observability, and runbook files.
7. Developer runs `attesta check payments-api`.
8. Readiness evaluation runs as a workflow and returns production-readiness score, recommendations, and workflow evidence.
9. Local CI/CD runner builds images, deploys stable and canary revisions to kind, and verifies runtime health.
10. Health gate runs as a workflow that evaluates canary latency/error signals and records rollback intent if the policy breaches.
11. If the gate fails, the runner executes `kubectl rollout undo`.
12. Incident ingestion runs as a workflow that evaluates SLO impact, enriches service ownership, generates a timeline, and stores the incident record durably.

## Controller Reconciliation

The `attesta-controller` process is the Kubernetes reconciliation layer. It watches `RolloutGuard` resources, resolves the referenced Deployment, evaluates live rollout state, updates `status.conditions`, emits Kubernetes Events, and restores the previous owned ReplicaSet template when a guarded rollout is blocked and rollback is enabled.

This keeps responsibilities separated:

- Attesta API and Helios-style workflows execute explicit reliability operations and expose audit evidence.
- The controller continuously reconciles Kubernetes desired state and actual state.
- CRDs remain the cluster-native contract between service teams, GitOps, and Attesta automation.

## Workflow Boundaries

Attesta does not run a second standalone control plane next to Helios. Instead, Attesta embeds Helios-style orchestration where reliability operations need ordered execution and a persisted audit trail:

- `attesta.service.onboarding`: validates and creates service metadata, records initial deployment, renders templates.
- `attesta.readiness.check`: evaluates production-readiness policy.
- `attesta.slo.evaluate`: evaluates SLO/error-budget state and deployment gate.
- `attesta.rollout.start`: checks deployment eligibility and records rollout start.
- `attesta.rollout.health-gate`: evaluates canary metrics and records rollback intent on breach.
- `attesta.rollout.rollback`: records rollback intent for GitOps/Kubernetes execution.
- `attesta.incident.enrichment`: correlates alerts with service metadata, SLO impact, RCA timeline, and postmortem context.

## Durable State

With PostgreSQL enabled, Attesta persists:

- service catalog and deployment history
- workflow runs, steps, events, and idempotency keys
- incident records, enriched SLO signals, and RCA timeline events

Workflow idempotency lookup is durable, so repeated operations with the same idempotency key can be recognized after an API restart. Service onboarding uses a deterministic key derived from the normalized service spec.

The embedded runner is not a crash-resumable workflow engine. A process exit can leave the last persisted run in `running`, and the current implementation does not resume from the last completed step. Persistence failures fail the active request closed and are returned as workflow failures; they are not silently presented as successful durable execution. Operations that require crash recovery should be delegated to an external workflow engine or wait for a future resumable runner.

## Local-First Production Design

Attesta avoids paid services. It runs locally with Go, PostgreSQL through Docker Compose, kind, kubectl, Kustomize, and optional Trivy. The architecture still maps cleanly to production platforms because the generated contracts are Kubernetes-native and GitOps-friendly.
