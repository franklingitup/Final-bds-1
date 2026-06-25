# Outputs consumed by the installer CLI to retrieve kubeconfig and bootstrap the agent.
output "cluster_name" {
  value       = var.cluster_name
  description = "Cluster name."
}

output "cluster_endpoint" {
  value       = ""
  description = "Kubernetes API endpoint (populated by aws_eks_cluster)."
}

output "cluster_ca_certificate" {
  value       = ""
  description = "Base64 CA certificate (populated by aws_eks_cluster)."
  sensitive   = true
}
