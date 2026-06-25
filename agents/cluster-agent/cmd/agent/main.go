// Command agent is the entrypoint for the in-cluster reconciler that runs in
// customer Kubernetes clusters. It connects outbound-only to the control plane.
// See docs/08-agent-design.md. Business logic is intentionally omitted.
package main

import (
	"log/slog"
	"net/http"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "cluster-agent")
	slog.SetDefault(logger)

	endpoint := os.Getenv("CONTROL_PLANE_ENDPOINT")
	slog.Info("starting cluster agent", "controlPlane", endpoint)

	// Operational endpoints for liveness/readiness probes.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ready")) })

	// TODO: register controller; run registration -> heartbeat -> reconcile loops.
	if err := http.ListenAndServe(":8080", mux); err != nil {
		slog.Error("agent stopped", "error", err)
	}
}
