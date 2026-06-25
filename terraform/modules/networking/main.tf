# Shared networking primitives skeleton. Cloud-specific modules wrap this with
# provider resources. Derives subnet CIDRs from the parent VPC CIDR.

locals {
  subnets = [for i in range(var.subnet_count) : cidrsubnet(var.vpc_cidr, 4, i)]
}

# TODO: provider-specific VPC/VNet + subnet resources consume local.subnets.
