# Example invocation of the EKS module for local validation / reference.
module "eks" {
  source             = "../../modules/eks"
  cluster_name       = "example-eks"
  region             = "us-east-1"
  kubernetes_version = "1.30"
  node_pools = [
    {
      name          = "default"
      instance_type = "m6i.large"
      min           = 2
      max           = 6
    }
  ]
  networking = {
    vpc_cidr = "10.0.0.0/16"
  }
}
