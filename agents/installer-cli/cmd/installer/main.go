// Command installer is the binary the generated install command runs on the
// customer machine. It performs: prerequisite checks -> tofu init/plan/apply ->
// kubeconfig retrieval -> agent Helm install -> registration callback.
// See docs/07-cluster-engine-design.md section 3. Logic is intentionally omitted.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: installer <session-id>")
		fmt.Println("env: PLATFORM_INSTALL_TOKEN, CONTROL_PLANE_ENDPOINT")
		os.Exit(2)
	}

	// TODO: implement staged installer flow with resumable steps.
	steps := []string{
		"check-prerequisites",
		"tofu-init",
		"tofu-plan",
		"tofu-apply",
		"fetch-kubeconfig",
		"install-agent",
		"register-callback",
	}
	for _, s := range steps {
		fmt.Printf("[skeleton] step pending: %s\n", s)
	}
}
