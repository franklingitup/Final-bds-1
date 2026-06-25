// Package agent contains the reconciler controllers and control-plane client.
// See docs/08-agent-design.md sections 2-8.
package agent

// Controller reconciles desired state pulled from the control plane into native
// Kubernetes resources. Implementations are intentionally omitted in this skeleton.
type Controller interface {
	// Register exchanges the one-time install token for an agent credential.
	Register() error
	// Heartbeat reports health and capabilities to the control plane.
	Heartbeat() error
	// Reconcile pulls desired state and applies it; reports actual state back.
	Reconcile() error
}
