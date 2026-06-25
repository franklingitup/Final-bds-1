# Installs the BDS cluster agent into a freshly provisioned cluster.
# The installer CLI runs this after the cluster module completes.

# TODO: configure helm provider against the new cluster's kubeconfig.
# resource "helm_release" "agent" {
#   name       = "bds-agent"
#   chart      = "bds-agent"
#   namespace  = "platform-agent"
#   create_namespace = true
#   version    = var.agent_chart_version
#   set {
#     name  = "controlPlane.endpoint"
#     value = var.control_plane_endpoint
#   }
#   set_sensitive {
#     name  = "controlPlane.installToken"
#     value = var.install_token
#   }
# }
