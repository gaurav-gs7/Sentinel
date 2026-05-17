# Local CI/CD And Deployment Runner

`scripts/local-ci-cd.sh` is the full laptop-local proof path for Sentinel.

It intentionally mirrors a production delivery flow without requiring paid services.

## Flow

1. Run preflight checks for Go, Docker, kind, kubectl, curl, and perl.
2. Start the Sentinel API if needed.
3. Onboard `payments-api` through `sentinel service init`.
4. Validate generated service guardrails.
5. Run generated service tests.
6. Render Kustomize manifests.
7. Build stable and canary Docker image tags.
8. Run Trivy if installed, otherwise record a scan skip.
9. Create or reuse the `sentinel-local` kind cluster.
10. Load images into kind.
11. Deploy the stable revision.
12. Deploy the canary revision.
13. Verify `/readyz` and `/metrics`.
14. Send a failing canary signal to Sentinel.
15. Record rollback intent.
16. Execute `kubectl rollout undo`.
17. Verify the live deployment image is back to the stable tag.
18. Run readiness and SLO status checks.
19. Create an incident from an Alertmanager-style signal.
20. Generate incident timeline and postmortem output.
21. List Helios-style workflow runs to show the execution trail behind onboarding, checks, rollback, and incident enrichment.

## Evidence

Each run writes evidence under:

```text
artifacts/local-ci-cd/<run-id>/
```

Key files:

- `pipeline.log`
- `summary.json`
- `rendered-stable.yaml`
- `rendered-canary.yaml`
- `rollback.json`
- `incident.json`
- workflow output in `pipeline.log`
- `trivy-skipped.txt` when Trivy is not installed
