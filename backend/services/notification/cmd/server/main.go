// Command server is the entrypoint for the notification service.
package main

import (
	"log/slog"
	"os"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/httpserver"
	notification "github.com/bdsplatform/platform/backend/services/notification/internal"
)

func main() {
	cfg := config.MustLoad("notification")
	if err := httpserver.Run(cfg, notification.RegisterRoutes); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
