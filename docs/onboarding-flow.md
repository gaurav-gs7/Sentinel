# Service Onboarding Flow

1. Developer runs `sentinel service init payments-api`.
2. CLI sends metadata to `POST /api/v1/services`.
3. API validates ownership, tier, pager, naming, SLO, and deployment strategy.
4. A Helios-style workflow run starts for onboarding.
5. Workflow steps store the service, record the initial generated deployment, and render the scaffold.
6. Template engine generates source, Dockerfile, CI, Kustomize overlays, Sentinel CR-style resources, observability, Terraform, Argo CD, and runbooks.
7. Developer commits generated assets to the service repository or GitOps repository.
8. GitHub Actions builds, tests, scans, and publishes the image.
9. Argo CD syncs the Kubernetes desired state.
10. Sentinel readiness, SLO, rollout, and incident workflows operate the service after deployment.
