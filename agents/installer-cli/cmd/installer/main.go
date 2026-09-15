// Command installer provisions a cluster and installs its platform agent.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bdsplatform/platform/agents/installer-cli/internal/agent"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Installation failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	token := strings.TrimSpace(os.Getenv("PLATFORM_INSTALL_TOKEN"))
	if token == "" {
		return fmt.Errorf("PLATFORM_INSTALL_TOKEN is required")
	}
	baseURL := strings.TrimSpace(os.Getenv("CONTROL_PLANE_ENDPOINT"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("PLATFORM_URL"))
	}
	if baseURL == "" {
		baseURL = agent.DefaultControlPlaneEndpoint
	}

	fmt.Println("BDS Platform cluster installer")
	installer := agent.NewInstaller(agent.NewClient(baseURL, 30*time.Second))
	return installer.Run(ctx, token)
}
