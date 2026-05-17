#!/usr/bin/env bash
set -euo pipefail

cluster_name="${1:-sentinel}"

kind create cluster --name "$cluster_name" --wait 120s
kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -

echo "kind cluster '$cluster_name' is ready"
