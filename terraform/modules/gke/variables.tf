# Uniform cluster-module variable contract (shared across eks/gke/aks).
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

variable "project_id" {
  type        = string
  description = "GCP project ID."
}

variable "tags" {
  type        = map(string)
  description = "Resource labels."
  default     = {}
}
