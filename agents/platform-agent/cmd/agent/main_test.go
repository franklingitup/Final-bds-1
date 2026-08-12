package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestHealthHandler_LivenessAlwaysOK proves /healthz returns 200 even before the
// agent is registered — so the kubelet never kills a cold agent that is still
// establishing (or backing off on) registration. This is the regression guard
// for the missing-health-endpoint CrashLoopBackOff bug.
func TestHealthHandler_LivenessAlwaysOK(t *testing.T) {
	h := healthHandler(func() bool { return false })
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", resp.StatusCode)
	}
}

// TestHealthHandler_ReadinessTracksRegistration proves /readyz is 503 before
// registration and 200 after, so rollouts wait for a working agent.
func TestHealthHandler_ReadinessTracksRegistration(t *testing.T) {
	var ready atomic.Bool
	h := healthHandler(ready.Load)
	srv := httptest.NewServer(h)
	defer srv.Close()

	get := func(path string) int {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := get("/readyz"); code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz before registration = %d, want 503", code)
	}

	ready.Store(true)
	if code := get("/readyz"); code != http.StatusOK {
		t.Fatalf("/readyz after registration = %d, want 200", code)
	}
}

// TestHealthHandler_MetricsServed proves /metrics is exposed on the same port as
// the probes (so annotation/ServiceMonitor scraping and probes align).
func TestHealthHandler_MetricsServed(t *testing.T) {
	h := healthHandler(func() bool { return true })
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics = %d, want 200", resp.StatusCode)
	}
}

// TestHealthHandler_NilReadyIsNotReady guards against a nil readiness func
// producing a false-positive Ready.
func TestHealthHandler_NilReadyIsNotReady(t *testing.T) {
	h := healthHandler(nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz with nil ready = %d, want 503", resp.StatusCode)
	}
}
