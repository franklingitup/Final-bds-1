package provisioning

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/middleware"
)

type bundleSessionStore struct {
	byToken map[string]*InstallSession
}

func (s *bundleSessionStore) Create(context.Context, *InstallSession) error { return nil }
func (s *bundleSessionStore) GetByID(context.Context, string) (*InstallSession, error) {
	return nil, database.ErrNotFound
}
func (s *bundleSessionStore) GetByToken(_ context.Context, token string) (*InstallSession, error) {
	session, ok := s.byToken[token]
	if !ok {
		return nil, database.ErrNotFound
	}
	return session, nil
}
func (s *bundleSessionStore) GetByRequestID(context.Context, string) (*InstallSession, error) {
	return nil, database.ErrNotFound
}
func (s *bundleSessionStore) List(context.Context, string, database.PageRequest) (database.Page[InstallSession], error) {
	return database.Page[InstallSession]{}, nil
}
func (s *bundleSessionStore) Update(context.Context, *InstallSession) error { return nil }
func (s *bundleSessionStore) UpdateStatus(context.Context, string, string) error {
	return nil
}
func (s *bundleSessionStore) ExpireOldSessions(context.Context) (int, error) { return 0, nil }

type bundleRequestStore struct {
	byID map[string]*ProvisioningRequest
}

func (s *bundleRequestStore) Create(context.Context, *ProvisioningRequest) error { return nil }
func (s *bundleRequestStore) GetByID(_ context.Context, id string) (*ProvisioningRequest, error) {
	req, ok := s.byID[id]
	if !ok {
		return nil, database.ErrNotFound
	}
	return req, nil
}
func (s *bundleRequestStore) GetByName(context.Context, string) (*ProvisioningRequest, error) {
	return nil, database.ErrNotFound
}
func (s *bundleRequestStore) List(context.Context, string, database.PageRequest) (database.Page[ProvisioningRequest], error) {
	return database.Page[ProvisioningRequest]{}, nil
}
func (s *bundleRequestStore) ListByStatus(context.Context, string, []string) ([]ProvisioningRequest, error) {
	return nil, nil
}
func (s *bundleRequestStore) Update(context.Context, *ProvisioningRequest) error { return nil }
func (s *bundleRequestStore) UpdateStatus(context.Context, string, string, *string) error {
	return nil
}
func (s *bundleRequestStore) Delete(context.Context, string) error { return nil }

type bundleCredentialStore struct {
	credential *CloudCredential
}

func (s *bundleCredentialStore) Create(context.Context, *CloudCredential) error { return nil }
func (s *bundleCredentialStore) GetByID(context.Context, string) (*CloudCredential, error) {
	return s.credential, nil
}
func (s *bundleCredentialStore) GetByName(context.Context, string) (*CloudCredential, error) {
	return nil, database.ErrNotFound
}
func (s *bundleCredentialStore) List(context.Context, string) ([]CloudCredential, error) {
	return nil, nil
}
func (s *bundleCredentialStore) ListByProvider(context.Context, string, string) ([]CloudCredential, error) {
	return nil, nil
}
func (s *bundleCredentialStore) Update(context.Context, *CloudCredential) error { return nil }
func (s *bundleCredentialStore) Delete(context.Context, string) error           { return nil }

type bundleTenantRunner struct{}

func (bundleTenantRunner) WithTenant(ctx context.Context, _ string, fn database.TxFunc) error {
	return fn(ctx)
}

type bundleClusterClient struct {
	status *ClusterStatus
}

func (c *bundleClusterClient) CreateClusterWithToken(context.Context, string, ClusterCreateRequest) (*ClusterCreateResponse, error) {
	return nil, nil
}

func (c *bundleClusterClient) GetCluster(context.Context, string, string) (*ClusterStatus, error) {
	return c.status, nil
}

func newBundleTestService(expiresAt time.Time) *Service {
	tf := "terraform { required_version = \">= 1.5\" }"
	vars := json.RawMessage(`{"cluster_name":"test-cluster"}`)
	bootstrap := "bootstrap-token"
	steps, _ := json.Marshal(DefaultInstallSteps)
	return NewService(Deps{
		Requests: &bundleRequestStore{byID: map[string]*ProvisioningRequest{
			"req-1": {
				ID:              "req-1",
				OrgID:           "org-1",
				Provider:        ProviderAWS,
				TerraformConfig: &tf,
				TerraformVars:   vars,
			},
		}},
		Sessions: &bundleSessionStore{byToken: map[string]*InstallSession{
			"session-token": {
				ID:             "session-1",
				OrgID:          "org-1",
				RequestID:      "req-1",
				SessionToken:   "session-token",
				BootstrapToken: &bootstrap,
				Steps:          steps,
				Status:         SessionActive,
				ExpiresAt:      expiresAt,
			},
		}},
		Tenant: bundleTenantRunner{},
	})
}

func TestGetSessionBundleValidToken(t *testing.T) {
	svc := newBundleTestService(time.Now().Add(time.Hour))

	bundle, err := svc.GetSessionBundle(context.Background(), "session-token")
	if err != nil {
		t.Fatalf("GetSessionBundle: %v", err)
	}
	if bundle.SessionID != "session-1" || bundle.Provider != ProviderAWS {
		t.Fatalf("unexpected bundle identity: %+v", bundle)
	}
	if bundle.TerraformConfig == "" || string(bundle.TerraformVars) != `{"cluster_name":"test-cluster"}` {
		t.Fatalf("unexpected Terraform bundle: %+v", bundle)
	}
	if bundle.BootstrapToken != "bootstrap-token" || len(bundle.Steps) != len(DefaultInstallSteps) {
		t.Fatalf("unexpected installer data: %+v", bundle)
	}
}

func TestGetSessionBundleSynchronizesRealAgentRegistration(t *testing.T) {
	svc := newBundleTestService(time.Now().Add(time.Hour))
	clusterID := "cluster-1"
	svc.requests.(*bundleRequestStore).byID["req-1"].ClusterID = &clusterID
	svc.clusterClient = &bundleClusterClient{status: &ClusterStatus{
		ID:                clusterID,
		Status:            "connected",
		AgentID:           "agent-1",
		KubernetesVersion: "1.30",
	}}

	bundle, err := svc.GetSessionBundle(context.Background(), "session-token")
	if err != nil {
		t.Fatalf("GetSessionBundle: %v", err)
	}
	if !bundle.AgentConnected {
		t.Fatal("bundle did not reflect real cluster-service agent registration")
	}
	if bundle.AgentVersion == nil || *bundle.AgentVersion != "1.30" {
		t.Fatalf("agentVersion = %v, want 1.30", bundle.AgentVersion)
	}
}

func TestGetSessionBundleIncludesSelectedCredentialEnvironment(t *testing.T) {
	svc := newBundleTestService(time.Now().Add(time.Hour))
	credentialID := "credential-1"
	svc.requests.(*bundleRequestStore).byID["req-1"].CredentialID = &credentialID
	svc.credentials = &bundleCredentialStore{credential: &CloudCredential{
		ID:          credentialID,
		Provider:    ProviderAWS,
		Credentials: []byte(`{"accessKeyId":"AKIA_TEST","secretAccessKey":"secret","sessionToken":"session"}`),
	}}
	svc.encryptor = &NoopEncryptor{}

	bundle, err := svc.GetSessionBundle(context.Background(), "session-token")
	if err != nil {
		t.Fatalf("GetSessionBundle: %v", err)
	}
	if bundle.CredentialEnv["AWS_ACCESS_KEY_ID"] != "AKIA_TEST" ||
		bundle.CredentialEnv["AWS_SECRET_ACCESS_KEY"] != "secret" ||
		bundle.CredentialEnv["AWS_SESSION_TOKEN"] != "session" {
		t.Fatalf("unexpected credential environment: %#v", bundle.CredentialEnv)
	}
}

func TestGetSessionBundleExpiredTokenRejected(t *testing.T) {
	svc := newBundleTestService(time.Now().Add(-time.Minute))
	if _, err := svc.GetSessionBundle(context.Background(), "session-token"); err == nil {
		t.Fatal("expected expired token rejection")
	}
}

func TestGetSessionBundleUnknownTokenRejected(t *testing.T) {
	svc := newBundleTestService(time.Now().Add(time.Hour))
	if _, err := svc.GetSessionBundle(context.Background(), "unknown"); err == nil {
		t.Fatal("expected unknown token rejection")
	}
}

func TestGetSessionBundleHTTPUsesSessionTokenWithoutJWT(t *testing.T) {
	svc := newBundleTestService(time.Now().Add(time.Hour))
	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})
	app.Get("/v1/sessions/:sessionToken/bundle", NewHandler(svc).GetSessionBundle)

	valid, err := app.Test(httptest.NewRequest(http.MethodGet, "/v1/sessions/session-token/bundle", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer valid.Body.Close()
	if valid.StatusCode != http.StatusOK {
		t.Fatalf("token-only request status = %d, want 200", valid.StatusCode)
	}

	withJWT := httptest.NewRequest(http.MethodGet, "/v1/sessions/not-a-session-token/bundle", nil)
	withJWT.Header.Set("Authorization", "Bearer unrelated-user-jwt")
	rejected, err := app.Test(withJWT)
	if err != nil {
		t.Fatal(err)
	}
	defer rejected.Body.Close()
	if rejected.StatusCode != http.StatusNotFound {
		t.Fatalf("JWT without valid session token status = %d, want 404", rejected.StatusCode)
	}
}

// --- Install Script Tests ---

type installSessionStore struct {
	byID    map[string]*InstallSession
	byToken map[string]*InstallSession
}

func (s *installSessionStore) Create(context.Context, *InstallSession) error { return nil }
func (s *installSessionStore) GetByID(_ context.Context, id string) (*InstallSession, error) {
	session, ok := s.byID[id]
	if !ok {
		return nil, database.ErrNotFound
	}
	return session, nil
}
func (s *installSessionStore) GetByToken(_ context.Context, token string) (*InstallSession, error) {
	session, ok := s.byToken[token]
	if !ok {
		return nil, database.ErrNotFound
	}
	return session, nil
}
func (s *installSessionStore) GetByRequestID(context.Context, string) (*InstallSession, error) {
	return nil, database.ErrNotFound
}
func (s *installSessionStore) List(context.Context, string, database.PageRequest) (database.Page[InstallSession], error) {
	return database.Page[InstallSession]{}, nil
}
func (s *installSessionStore) Update(context.Context, *InstallSession) error { return nil }
func (s *installSessionStore) UpdateStatus(context.Context, string, string) error {
	return nil
}
func (s *installSessionStore) ExpireOldSessions(context.Context) (int, error) { return 0, nil }

func newInstallScriptTestService(session *InstallSession) *Service {
	return NewService(Deps{
		Requests: &bundleRequestStore{byID: map[string]*ProvisioningRequest{}},
		Sessions: &installSessionStore{
			byID:    map[string]*InstallSession{session.ID: session},
			byToken: map[string]*InstallSession{session.SessionToken: session},
		},
		Tenant: bundleTenantRunner{},
	})
}

func TestGetInstallScriptValidSession(t *testing.T) {
	session := &InstallSession{
		ID:           "session-123",
		OrgID:        "org-1",
		SessionToken: "token-abc",
		Status:       SessionActive,
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	svc := newInstallScriptTestService(session)
	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})
	app.Get("/v1/install/:sessionId", NewHandler(svc).GetInstallScript)

	req := httptest.NewRequest(http.MethodGet, "/v1/install/session-123", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/x-shellscript" {
		t.Fatalf("expected Content-Type text/x-shellscript, got %s", contentType)
	}

	cacheControl := resp.Header.Get("Cache-Control")
	if cacheControl != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %s", cacheControl)
	}
}

func TestGetInstallScriptSessionNotFound(t *testing.T) {
	session := &InstallSession{
		ID:           "session-123",
		OrgID:        "org-1",
		SessionToken: "token-abc",
		Status:       SessionActive,
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	svc := newInstallScriptTestService(session)
	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})
	app.Get("/v1/install/:sessionId", NewHandler(svc).GetInstallScript)

	req := httptest.NewRequest(http.MethodGet, "/v1/install/unknown-session", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetInstallScriptExpiredSession(t *testing.T) {
	session := &InstallSession{
		ID:           "session-123",
		OrgID:        "org-1",
		SessionToken: "token-abc",
		Status:       SessionActive,
		ExpiresAt:    time.Now().Add(-time.Hour), // Expired
	}
	svc := newInstallScriptTestService(session)
	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})
	app.Get("/v1/install/:sessionId", NewHandler(svc).GetInstallScript)

	req := httptest.NewRequest(http.MethodGet, "/v1/install/session-123", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGone {
		t.Fatalf("expected 410 Gone, got %d", resp.StatusCode)
	}
}

func TestGetInstallScriptCompletedSession(t *testing.T) {
	session := &InstallSession{
		ID:           "session-123",
		OrgID:        "org-1",
		SessionToken: "token-abc",
		Status:       SessionCompleted,
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	svc := newInstallScriptTestService(session)
	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})
	app.Get("/v1/install/:sessionId", NewHandler(svc).GetInstallScript)

	req := httptest.NewRequest(http.MethodGet, "/v1/install/session-123", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Completed sessions should return validation error (422)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
}

func TestGetInstallScriptContainsSessionID(t *testing.T) {
	session := &InstallSession{
		ID:           "session-456",
		OrgID:        "org-1",
		SessionToken: "token-xyz",
		Status:       SessionActive,
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	svc := newInstallScriptTestService(session)
	app := fiber.New(fiber.Config{
		ErrorHandler: middleware.ErrorHandler(),
	})
	app.Get("/v1/install/:sessionId", NewHandler(svc).GetInstallScript)

	req := httptest.NewRequest(http.MethodGet, "/v1/install/session-456", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)

	// The script should contain the session ID
	if !strings.Contains(script, "session-456") {
		t.Fatal("script should contain session ID")
	}

	// The script should NOT contain the session token (it comes from env var)
	if strings.Contains(script, "token-xyz") {
		t.Fatal("script should NOT embed the session token")
	}
	if !strings.Contains(script, "/v1/agent-install/${BOOTSTRAP_TOKEN}/manifest.yaml") {
		t.Fatal("script should install the real cluster-service agent manifest")
	}
	if strings.Contains(script, "/v1/bootstrap/${BOOTSTRAP_TOKEN}/manifest.yaml") {
		t.Fatal("script should not reference the removed provisioning bootstrap manifest")
	}
	if !strings.Contains(script, "configure_cloud_credentials") {
		t.Fatal("script should configure the selected cloud credential")
	}
}
