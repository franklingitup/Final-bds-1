# GKE module skeleton.
# Provisions: VPC, subnets, service accounts, GKE cluster, node pools, and
# workload identity. Resources are intentionally omitted in this skeleton.

provider "google" {
  project = var.project_id
  region  = var.region
}

locals {
  name = var.cluster_name
}

# TODO: google_compute_network + subnetwork (var.networking.vpc_cidr)
# TODO: google_container_cluster.this (var.kubernetes_version, workload identity)
# TODO: google_container_node_pool.this (for_each over var.node_pools)
# TODO: service accounts with least-privilege IAM
