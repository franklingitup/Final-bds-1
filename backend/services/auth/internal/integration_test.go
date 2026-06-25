//go:build integration

// Integration tests exercise the auth service against a real PostgreSQL
// instance (transactional outbox + tenant-scoped service accounts). They are
// skipped unless a database is reachable. Run with:
//
//	DATABASE_URL=postgres://... go test -tags=integration ./services/auth/...
package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	"github.com/bdsplatform/platform/backend/libs/events"
	"github.com/bdsplatform/platform/backend/migrations"
)

func setupAuthIntegration(t *testing.T) (*Service, *database.DB) {
	t.Helper()
	cfg, err := config.Load("auth")
	if err != nil {
		t.Skipf("config unavailable: %v", err)
	}
	ctx := context.Background()
	db, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	t.Cleanup(db.Close)

	for _, m := range []struct{ service, table string }{
		{"auth", "schema_migrations_auth"},
		{"outbox", "schema_migrations_outbox"},
	} {
		fsys, err := migrations.Service(m.service)
		if err != nil {
			t.Fatalf("migrations.Service(%s): %v", m.service, err)
		}
		migs, err := database.LoadMigrations(fsys)
		if err != nil {
			t.Fatalf("load migrations: %v", err)
		}
		migrator, err := database.NewMigrator(db, m.table, migs)
		if err != nil {
			t.Fatalf("new migrator: %v", err)
		}
		if _, err := migrator.Up(ctx); err != nil {
			t.Fatalf("migrate up: %v", err)
		}
	}

	svc := NewService(Deps{
		Users:           NewUserStore(db),
		Sessions:        NewSessionStore(db),
		OneTimeTokens:   NewOneTimeTokenStore(db),
		ServiceAccounts: NewServiceAccountStore(db),
		APITokens:       NewAPITokenStore(db),
		Tx:              db,
		Tenant:          db,
		JWT:             NewJWTIssuer(testAuthConfig()),
		Outbox:          events.NewPostgresOutbox(db, "outbox"),
		Auth:            testAuthConfig(),
	})
	return svc, db
}

func assertOutboxHasGlobal(t *testing.T, db *database.DB, eventType string, resourceID string) {
	t.Helper()
	var n int
	err := db.Pool.QueryRow(context.Background(),
		"SELECT count(*) FROM outbox WHERE event_type = $1 AND envelope->'resource'->>'id' = $2",
		eventType, resourceID).Scan(&n)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if n == 0 {
		t.Errorf("expected an outbox row for %s (resource %s)", eventType, resourceID)
	}
}

func TestIntegration_MFALifecycle(t *testing.T) {
	svc, db := setupAuthIntegration(t)
	ctx := context.Background()
	email := "mfa+" + uuid.NewString()[:8] + "@example.com"

	pair, err := svc.Signup(ctx, SignupRequest{Email: email, Password: "password123", Name: "MFA User"}, RequestMeta{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	userID := pair.User.ID

	setup, err := svc.SetupMFA(ctx, userID)
	if err != nil {
		t.Fatalf("setup mfa: %v", err)
	}
	assertOutboxHasGlobal(t, db, EventMFASetupStarted, userID)

	code, _ := totpCodeAt(setup.Secret, svc.now())
	if err := svc.EnableMFA(ctx, userID, code); err != nil {
		t.Fatalf("enable mfa: %v", err)
	}
	assertOutboxHasGlobal(t, db, EventMFAEnabled, userID)

	code, _ = totpCodeAt(setup.Secret, svc.now())
	if err := svc.DisableMFA(ctx, userID, code); err != nil {
		t.Fatalf("disable mfa: %v", err)
	}
	assertOutboxHasGlobal(t, db, EventMFADisabled, userID)
}

func TestIntegration_TokenRotated(t *testing.T) {
	svc, db := setupAuthIntegration(t)
	ctx := context.Background()
	email := "rotate+" + uuid.NewString()[:8] + "@example.com"

	pair, err := svc.Signup(ctx, SignupRequest{Email: email, Password: "password123", Name: "Rot User"}, RequestMeta{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if _, err := svc.Refresh(ctx, RefreshRequest{RefreshToken: pair.RefreshToken}, RequestMeta{}); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	assertOutboxHasGlobal(t, db, EventTokenRotated, pair.User.ID)
}

func TestIntegration_ServiceAccountLifecycle(t *testing.T) {
	svc, db := setupAuthIntegration(t)
	ctx := context.Background()
	orgID := uuid.NewString()
	creator := uuid.NewString()

	sa, err := svc.CreateServiceAccount(ctx, orgID, creator, CreateServiceAccountRequest{Name: "ci-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatalf("create service account: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, orgID, func(ctx context.Context) error {
			_, err := db.Conn(ctx).Exec(ctx, "DELETE FROM service_accounts WHERE id = $1", sa.ID)
			return err
		})
	})
	assertOutboxHasGlobal(t, db, EventServiceAccountCreated, sa.ID)

	if err := svc.DeleteServiceAccount(ctx, orgID, sa.ID); err != nil {
		t.Fatalf("delete service account: %v", err)
	}
	assertOutboxHasGlobal(t, db, EventServiceAccountDeleted, sa.ID)
}
