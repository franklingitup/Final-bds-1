package provisioning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

type bundleTenantRunner struct{}

func (bundleTenantRunner) WithTenant(ctx context.Context, _ string, fn database.TxFunc) error {
	return fn(ctx)
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
