package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bdsplatform/platform/agents/platform-agent/internal/config"
	"github.com/bdsplatform/platform/agents/platform-agent/internal/controlplane"
)

// cpStub is a configurable control-plane test double for the agent registration
// and recovery endpoints. Each field controls the HTTP status (and, for 200s,
// the returned cluster identity) so tests can exercise fresh registration,
// idempotent recovery, unrecoverable tokens, and transient outages.
type cpStub struct {
	registerStatus int
	recoverStatus  int
	clusterID      string
	orgID          string
	agentID        string // AgentID the server reports (authoritative identity)

	registerCalls atomic.Int32
	recoverCalls  atomic.Int32
}

func (s *cpStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/agent/register", func(w http.ResponseWriter, r *http.Request) {
		s.registerCalls.Add(1)
		s.write(w, s.registerStatus)
	})
	mux.HandleFunc("/v1/agent/recover", func(w http.ResponseWriter, r *http.Request) {
		s.recoverCalls.Add(1)
		s.write(w, s.recoverStatus)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (s *cpStub) write(w http.ResponseWriter, status int) {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status == http.StatusOK {
		_ = json.NewEncoder(w).Encode(controlplane.RegisterResponse{
			ID:             s.clusterID,
			OrganizationID: s.orgID,
			AgentID:        s.agentID,
			Name:           "test-cluster",
			Status:         "connected",
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "ERR", "message": "stub error"},
	})
}

func newTestAgent(t *testing.T, stub *cpStub, mutate func(*config.Config)) *Agent {
	t.Helper()
	srv := stub.server(t)
	cfg := config.Config{
		Token:                        "install-token",
		ControlPlaneURL:              srv.URL,
		HeartbeatInterval:            time.Hour,
		RegistrationRetryInterval:    5 * time.Millisecond,
		RegistrationMaxRetryInterval: 20 * time.Millisecond,
		RequestTimeout:               2 * time.Second,
		StateFile:                    filepath.Join(t.TempDir(), "state.json"),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	client := controlplane.NewClient(cfg.ControlPlaneURL, cfg.RequestTimeout)
	collector := &fakeInventoryCollector{}
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(cfg, client, collector, log)
}

// TestResolveAgentID_Precedence verifies the stable-identity ordering:
// persisted > configured (AGENT_ID) > pod UID > generated.
func TestResolveAgentID_Precedence(t *testing.T) {
	tests := []struct {
		name       string
		persisted  string
		configured string
		podUID     string
		wantExact  string // "" means "generated, just non-empty"
	}{
		{name: "persisted wins", persisted: "agent-persisted", configured: "agent-env", podUID: "uid-1", wantExact: "agent-persisted"},
		{name: "configured second", configured: "agent-env", podUID: "uid-1", wantExact: "agent-env"},
		{name: "pod uid third", podUID: "uid-1", wantExact: "agent-uid-1"},
		{name: "generated last", wantExact: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Agent{
				cfg:   config.Config{AgentID: tt.configured, PodUID: tt.podUID},
				state: &State{AgentID: tt.persisted},
				log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			a.resolveAgentID()
			if tt.wantExact != "" {
				if a.state.AgentID != tt.wantExact {
					t.Errorf("AgentID = %q, want %q", a.state.AgentID, tt.wantExact)
				}
			} else if a.state.AgentID == "" {
				t.Error("AgentID should be generated (non-empty)")
			}
		})
	}
}

// TestEnsureRegistered_Fresh verifies a first-boot registration adopts the
// control-plane identity and persists state.
func TestEnsureRegistered_Fresh(t *testing.T) {
	stub := &cpStub{registerStatus: 200, clusterID: "cluster-1", orgID: "org-1", agentID: "agent-1"}
	a := newTestAgent(t, stub, nil)
	a.resolveAgentID()

	if err := a.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}
	if !a.state.Registered || a.state.ClusterID != "cluster-1" || a.state.OrganizationID != "org-1" {
		t.Errorf("state not populated: %+v", a.state)
	}
	if a.state.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want adopted agent-1", a.state.AgentID)
	}
	// State must be persisted so a restart skips registration.
	reloaded, err := LoadState(a.cfg.StateFile)
	if err != nil || !reloaded.Registered || reloaded.ClusterID != "cluster-1" {
		t.Errorf("state not persisted: %+v err=%v", reloaded, err)
	}
}

// TestEnsureRegistered_RecoverOnConflict verifies that a 409 from register is
// resolved by recovering the existing cluster instead of crashing.
func TestEnsureRegistered_RecoverOnConflict(t *testing.T) {
	stub := &cpStub{
		registerStatus: http.StatusConflict,
		recoverStatus:  http.StatusOK,
		clusterID:      "cluster-9", orgID: "org-9", agentID: "agent-original",
	}
	a := newTestAgent(t, stub, nil)
	a.state.AgentID = "agent-regenerated" // lost-state: a new local id
	a.resolveAgentID()

	if err := a.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("ensureRegistered should recover, got: %v", err)
	}
	if a.state.ClusterID != "cluster-9" {
		t.Errorf("clusterID = %q, want cluster-9", a.state.ClusterID)
	}
	if a.state.AgentID != "agent-original" {
		t.Errorf("AgentID = %q, want adopted stable agent-original", a.state.AgentID)
	}
	if stub.recoverCalls.Load() == 0 {
		t.Error("recover endpoint was not called")
	}
}

// TestEnsureRegistered_InvalidTokenFatal verifies an unusable token (401 on both
// register and recover) terminates with an unrecoverable configuration error
// rather than looping forever.
func TestEnsureRegistered_InvalidTokenFatal(t *testing.T) {
	stub := &cpStub{registerStatus: http.StatusUnauthorized, recoverStatus: http.StatusUnauthorized}
	a := newTestAgent(t, stub, nil)
	a.resolveAgentID()

	err := a.ensureRegistered(context.Background())
	var fatal *fatalConfigError
	if err == nil {
		t.Fatal("expected fatalConfigError, got nil")
	}
	if !asFatal(err, &fatal) {
		t.Errorf("expected *fatalConfigError, got %T: %v", err, err)
	}
}

// TestEnsureRegistered_BackoffThenCancel verifies transient failures never crash
// the agent: it retries with backoff and exits cleanly when the context is
// cancelled.
func TestEnsureRegistered_BackoffThenCancel(t *testing.T) {
	stub := &cpStub{registerStatus: http.StatusInternalServerError, recoverStatus: http.StatusInternalServerError}
	a := newTestAgent(t, stub, nil)
	a.resolveAgentID()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	err := a.ensureRegistered(ctx)
	if err == nil {
		t.Fatal("expected context error after cancellation")
	}
	if a.state.Registered {
		t.Error("agent must not be registered after transient failures")
	}
	if stub.registerCalls.Load() < 2 {
		t.Errorf("expected multiple retry attempts, got %d", stub.registerCalls.Load())
	}
}

// TestEnsureRegistered_AlreadyRegistered verifies persisted state short-circuits
// registration without contacting the control plane.
func TestEnsureRegistered_AlreadyRegistered(t *testing.T) {
	stub := &cpStub{registerStatus: http.StatusInternalServerError}
	a := newTestAgent(t, stub, nil)
	a.state = &State{AgentID: "agent-x", ClusterID: "cluster-x", OrganizationID: "org-x", Registered: true}

	if err := a.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}
	if stub.registerCalls.Load() != 0 {
		t.Errorf("register must not be called when already registered, got %d calls", stub.registerCalls.Load())
	}
}

// TestRun_RecoversAfterStateLoss simulates deleting state.json: a fresh agent
// with the same token recovers the existing cluster and keeps a stable identity.
func TestRun_RecoversAfterStateLoss(t *testing.T) {
	stub := &cpStub{
		registerStatus: http.StatusConflict,
		recoverStatus:  http.StatusOK,
		clusterID:      "cluster-77", orgID: "org-77", agentID: "agent-stable",
	}
	a := newTestAgent(t, stub, nil)
	a.resolveAgentID() // no persisted state -> generated id

	if err := a.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}
	if a.state.ClusterID != "cluster-77" || a.state.AgentID != "agent-stable" {
		t.Errorf("recovery did not rebuild stable state: %+v", a.state)
	}
}

// asFatal reports whether err is a *fatalConfigError (kept local to avoid an
// extra import for a single call site).
func asFatal(err error, target **fatalConfigError) bool {
	f, ok := err.(*fatalConfigError)
	if ok {
		*target = f
	}
	return ok
}
