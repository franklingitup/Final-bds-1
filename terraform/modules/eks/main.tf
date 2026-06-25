# EKS module skeleton.
# Provisions: VPC, subnets, IAM roles/OIDC, EKS control plane, managed node
# groups, and core addons. Resources are intentionally omitted in this skeleton.

provider "aws" {
  region = var.region
}

locals {
  name = var.cluster_name
  tags = merge({ "platform/cluster" = var.cluster_name }, var.tags)
}

# TODO: module "vpc" { ... }                 # networking primitives
# TODO: aws_eks_cluster.this { ... }         # control plane (var.kubernetes_version)
# TODO: aws_eks_node_group.this (for_each over var.node_pools)
# TODO: IAM roles + OIDC provider for IRSA (least privilege)
# TODO: addons (vpc-cni, coredns, kube-proxy, ebs-csi)
