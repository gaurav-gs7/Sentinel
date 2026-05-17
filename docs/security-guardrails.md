# Security Guardrails

Sentinel starts with controls that are simple enough to run locally but map to real platform policy:

- Services must have an owner and team.
- Namespaces and service names must be valid Kubernetes DNS labels.
- SLO targets must stay in an operationally meaningful range.
- Generated Deployments include resource requests, limits, readiness probes, liveness probes, non-root execution, dropped capabilities, and read-only root filesystems.
- Generated CI runs Trivy and fails on critical image vulnerabilities.

Future phases can replace or augment the built-in Go validation with Kyverno, Gatekeeper, or Conftest policies without changing the onboarding API.
