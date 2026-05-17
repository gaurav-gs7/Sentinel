# Resume Impact

Sentinel — Kubernetes Reliability Platform with Helios-Style Workflow Orchestration

- Built a Go-based Kubernetes reliability platform that standardized service onboarding using CRDs, CLI workflows, GitOps templates, SLO policies, runbooks, dashboards, and production-readiness checks.
- Integrated a Helios-style workflow engine for onboarding, SLO checks, canary health gates, rollback decisions, incident enrichment, ordered execution steps, retry metadata, and audit events.
- Implemented a production-readiness engine that validates ownership, tier, pager, SLOs, rollout policy, rollback readiness, generated Kubernetes guardrails, observability defaults, and runbook coverage.
- Developed SLO/error-budget tracking to evaluate availability, p95 latency, error rate, burn rate, and deployment eligibility for Kubernetes services.
- Integrated local CI/CD, Kustomize, kind, Prometheus/Grafana templates, Alertmanager-style incident ingestion, automated rollback evidence, and RCA timeline/postmortem generation.

## Interview Pitch

I built Sentinel as a production-style Kubernetes reliability platform powered by a Helios-style workflow engine. It keeps service onboarding self-service, but every service gets reliability metadata, SLOs, rollback policy, generated Kubernetes guardrails, observability defaults, and runbooks from day one. The demo deploys a generated service to kind, promotes a canary, detects a bad canary signal, rolls back automatically, creates an incident, and shows the workflow execution trail behind the RCA timeline and postmortem draft.
