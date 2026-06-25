output "subnet_cidrs" {
  value       = local.subnets
  description = "Derived subnet CIDR blocks."
}
