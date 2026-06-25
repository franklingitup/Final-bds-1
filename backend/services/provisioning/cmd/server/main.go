// Command server is the entrypoint for the provisioning service.
package main

import (
	"log/slog"
	"os"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	provisioning "github.com/bdsplatform/platform/backend/services/provisioning/internal"
)

func main() {
	cfg := config.MustLoad("provisioning")
	if err := httpserver.Run(cfg, provisioning.RegisterRoutes); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
