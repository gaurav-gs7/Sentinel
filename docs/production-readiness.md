# Production Readiness

Sentinel defines production readiness as a service having enough metadata, Kubernetes safety controls, observability, SLOs, rollout policy, and incident ownership to be safely operated.

## Sentinel Service Checks

The API and CLI validate:

- service name and namespace are Kubernetes DNS labels
- owner and team are set
- tier is one of `critical`, `high`, `medium`, or `low`
- pager metadata exists
- repository is linked
- runbook and dashboard are linked
- availability SLO is configured
- p95 latency objective is configured
- error-rate threshold is configured
- rollback-on-failure is enabled for progressive delivery

## Generated Kubernetes Guardrails

Generated services include:

- readiness probe
- liveness probe
- CPU and memory requests
- CPU and memory limits
- non-root runtime
- read-only root filesystem
- privilege escalation disabled
- Linux capabilities dropped
- service account token automount disabled
- HPA
- PDB
- Prometheus scrape annotations

## Demo Evidence

`make demo-full` proves these controls by generating a service, validating the scaffold, rendering Kustomize, building images, deploying to kind, verifying runtime readiness and metrics, triggering a failed canary signal, rolling back, creating an incident, and generating a timeline/postmortem.
