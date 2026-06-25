variable "vpc_cidr" {
  type        = string
  description = "CIDR block for the cluster network."
}

variable "subnet_count" {
  type        = number
  description = "Number of subnets to derive from the VPC CIDR."
  default     = 3
}
