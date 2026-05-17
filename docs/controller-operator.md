# Sentinel Controller Operator

`sentinel-controller` is the Kubernetes reconciliation layer for Sentinel rollout policy. It watches `RolloutGuard` resources, compares the declared guard with the live Deployment state, writes status conditions, emits Kubernetes Events, and restores the previous ReplicaSet template when a guarded rollout is blocked and rollback is enabled.

## Control Loop

1. Watch `sentinel.io/v1` `RolloutGuard` resources.
2. Resolve `spec.deploymentRef`, or fall back to `spec.serviceRef`.
3. Read the matching `apps/v1` Deployment.
4. Evaluate rollout state from Deployment generation, updated replicas, available replicas, pause state, and `ProgressDeadlineExceeded` conditions.
5. Patch `RolloutGuard.status` with phase, reason, replica counts, timestamps, and conditions.
6. Emit Kubernetes Events such as `RolloutProgressing`, `RolloutHealthy`, `RolloutBlocked`, `RollbackTriggered`, and `RollbackFailed`.
7. If blocked and `spec.rollback.enabled` is true, find the previous owned ReplicaSet revision and update the Deployment pod template back to that revision.

## Run Locally

```bash
go run ./controller/cmd/sentinel-controller --kubeconfig "$HOME/.kube/config" --resync=30s
```

## Deploy To A Cluster

```bash
kubectl apply -k deploy/crds
kubectl apply -k deploy/controller
```

The deployment expects an image named `ghcr.io/example/sentinel-controller:latest`. Build and publish your own image with:

```bash
docker build -f controller/Dockerfile -t ghcr.io/example/sentinel-controller:latest .
```

## Status Contract

The controller writes `status.phase` as one of:

- `Progressing`
- `Healthy`
- `Blocked`
- `RolledBack`
- `RollbackFailed`

It also writes conditions:

- `Reconciled`
- `DeploymentFound`
- `Healthy`
- `Blocked`
- `RolledBack` when rollback is attempted

This keeps the CRD useful from standard Kubernetes tooling:

```bash
kubectl get rolloutguards -A
kubectl describe rolloutguard payments-api-rollout -n platform
```
