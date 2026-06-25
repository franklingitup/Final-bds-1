# Uniform cluster-module variable contract (shared across eks/gke/aks).
# See docs/07-cluster-engine-design.md section 4.

variable "cluster_name" {
  type        = string
  description = "Cluster name."
}

variable "region" {
  type        = string
  description = "Cloud region."
}

variable "kubernetes_version" {
  type        = string
  description = "Kubernetes version."
}

variable "node_pools" {
  description = "Node pool definitions."
  type = list(object({
    name          = string
    instance_type = string
    min           = number
    max           = number
  }))
}

variable "networking" {
  description = "Networking configuration."
  type = object({
    vpc_cidr = string
  })
}

variable "tags" {
  type        = map(string)
  description = "Resource tags."
  default     = {}
}
