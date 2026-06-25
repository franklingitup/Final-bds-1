// Command server is the entrypoint for the api-gateway service.
package main

import (
	"log/slog"
	"os"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	gateway "github.com/bdsplatform/platform/backend/services/api-gateway/internal"
)

func main() {
	cfg := config.MustLoad("api-gateway")
	if err := httpserver.Run(cfg, gateway.RegisterRoutes); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
