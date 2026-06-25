// Command server is the entrypoint for the observability service.
package main

import (
	"log/slog"
	"os"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	observability "github.com/bdsplatform/platform/backend/services/observability/internal"
)

func main() {
	cfg := config.MustLoad("observability")
	if err := httpserver.Run(cfg, observability.RegisterRoutes); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
