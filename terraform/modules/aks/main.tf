# AKS module skeleton.
# Provisions: VNet, subnets, managed identity, AKS cluster, node pools, and RBAC.
# Resources are intentionally omitted in this skeleton.

provider "azurerm" {
  features {}
}

locals {
  name = var.cluster_name
}

# TODO: azurerm_virtual_network + subnets (var.networking.vpc_cidr)
# TODO: azurerm_kubernetes_cluster.this (var.kubernetes_version, managed identity)
# TODO: azurerm_kubernetes_cluster_node_pool.this (for_each over var.node_pools)
# TODO: role assignments (least privilege)
