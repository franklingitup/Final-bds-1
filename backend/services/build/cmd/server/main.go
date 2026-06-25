// Command server is the entrypoint for the build service.
package main

import (
	"log/slog"
	"os"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	build "github.com/bdsplatform/platform/backend/services/build/internal"
)

func main() {
	cfg := config.MustLoad("build")
	if err := httpserver.Run(cfg, build.RegisterRoutes); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
