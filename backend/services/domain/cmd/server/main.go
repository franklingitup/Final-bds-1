// Command server is the entrypoint for the domain service.
package main

import (
	"log/slog"
	"os"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	domain "github.com/bdsplatform/platform/backend/services/domain/internal"
)

func main() {
	cfg := config.MustLoad("domain")
	if err := httpserver.Run(cfg, domain.RegisterRoutes); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
