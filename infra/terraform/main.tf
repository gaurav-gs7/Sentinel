terraform {
  required_version = ">= 1.6.0"
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.29"
    }
  }
}

variable "platform_namespaces" {
  type    = list(string)
  default = ["argocd", "monitoring", "sentinel-system"]
}

resource "kubernetes_namespace" "platform" {
  for_each = toset(var.platform_namespaces)

  metadata {
    name = each.value
    labels = {
      "sentinel.dev/managed" = "true"
    }
  }
}
