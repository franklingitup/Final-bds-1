output "cluster_name" {
  value       = var.cluster_name
  description = "Cluster name."
}

output "cluster_endpoint" {
  value       = ""
  description = "Kubernetes API endpoint (populated by azurerm_kubernetes_cluster)."
}

output "cluster_ca_certificate" {
  value       = ""
  description = "Base64 CA certificate (populated by azurerm_kubernetes_cluster)."
  sensitive   = true
}
