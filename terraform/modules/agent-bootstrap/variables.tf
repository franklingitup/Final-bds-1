variable "control_plane_endpoint" {
  type        = string
  description = "Control plane API endpoint the agent connects to (outbound)."
}

variable "install_token" {
  type        = string
  description = "One-time install token exchanged for an agent credential."
  sensitive   = true
}

variable "agent_chart_version" {
  type        = string
  description = "Version of the bds-agent Helm chart to install."
  default     = "0.1.0"
}
