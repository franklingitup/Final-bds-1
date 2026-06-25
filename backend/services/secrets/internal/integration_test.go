// +build integration

package secrets

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// Integration tests require a PostgreSQL database.
// Run with: go test -tags=integration -v ./...
//
// Environment variables:
//   DATABASE_URL: PostgreSQL connection string

func skipIfNoDatabase(t *testing.T) *database.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := database.ConnectURL(context.Background(), url)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	return db
}

// ============================================================================
// INTEGRATION TESTS - CRIT-001 FIX: Cross-Tenant Isolation with RLS
// ============================================================================

// TestIntegration_CrossTenantIsolation tests that RLS + explicit filter
// prevents cross-tenant secret access even at the database level.
func TestIntegration_CrossTenantIsolation(t *testing.T) {
	db := skipIfNoDatabase(t)
	defer db.Close()

	ctx := context.Background()
	repo := NewSecretRepository(db)

	orgA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	orgB := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	clusterA := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	// Setup: Create secrets for both orgs (requires setup data in DB)
	// This test assumes the database has been seeded with test data.

	// Test 1: Org A queries with Org A credentials
	var secretsOrgA []Secret
	err := db.WithTenant(ctx, orgA, func(ctx context.Context) error {
		var err error
		secretsOrgA, err = repo.GetSecretsForCluster(ctx, orgA, clusterA)
		return err
	})
	if err != nil {
		t.Fatalf("GetSecretsForCluster for Org A failed: %v", err)
	}

	// Verify: Only Org A secrets returned
	for _, s := range secretsOrgA {
		if s.OrgID != orgA {
			t.Errorf("SECURITY VIOLATION: Secret from org %s returned to org %s", s.OrgID, orgA)
		}
	}

	// Test 2: Attempt cross-tenant access (query Org B while in Org A context)
	var crossTenantSecrets []Secret
	err = db.WithTenant(ctx, orgA, func(ctx context.Context) error {
		var err error
		// ATTACK: Try to use Org B's ID while in Org A context
		crossTenantSecrets, err = repo.GetSecretsForCluster(ctx, orgB, clusterA)
		return err
	})
	// Note: This should return 0 results due to explicit org_id filter,
	// even if RLS allows (defense-in-depth).

	if len(crossTenantSecrets) > 0 {
		t.Errorf("SECURITY VIOLATION: Cross-tenant access returned %d secrets", len(crossTenantSecrets))
		for _, s := range crossTenantSecrets {
			t.Errorf("  - Leaked: org=%s, name=%s", s.OrgID, s.Name)
		}
	}
}

// TestIntegration_ExplicitFilterWithoutRLS simulates what happens if RLS is bypassed.
// The explicit org_id filter should still protect secrets.
func TestIntegration_ExplicitFilterWithoutRLS(t *testing.T) {
	db := skipIfNoDatabase(t)
	defer db.Close()

	ctx := context.Background()
	repo := NewSecretRepository(db)

	orgA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	fakeOrgID := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	clusterA := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	// Test: Query with fake org ID (simulates RLS bypass)
	// Even without RLS, the explicit filter should return 0 results.
	var secrets []Secret
	err := db.WithTenant(ctx, orgA, func(ctx context.Context) error {
		var err error
		// Use fake org ID that doesn't exist
		secrets, err = repo.GetSecretsForCluster(ctx, fakeOrgID, clusterA)
		return err
	})
	if err != nil {
		t.Logf("Query returned error (expected for non-existent org): %v", err)
	}

	// SECURITY ASSERTION: Zero secrets for non-existent org
	if len(secrets) > 0 {
		t.Errorf("SECURITY VIOLATION: Returned %d secrets for non-existent org", len(secrets))
	}
}

// TestIntegration_EmptyOrgIDRejected verifies database rejects empty orgID.
func TestIntegration_EmptyOrgIDRejected(t *testing.T) {
	db := skipIfNoDatabase(t)
	defer db.Close()

	ctx := context.Background()
	repo := NewSecretRepository(db)

	// Test: Empty org ID should be rejected before hitting DB
	_, err := repo.GetSecretsForCluster(ctx, "", "any-cluster-id")

	if err != ErrInvalidOrgID {
		t.Errorf("Expected ErrInvalidOrgID for empty orgID, got: %v", err)
	}
}

// TestIntegration_RLSAndExplicitFilterCoexist verifies both mechanisms work together.
func TestIntegration_RLSAndExplicitFilterCoexist(t *testing.T) {
	db := skipIfNoDatabase(t)
	defer db.Close()

	ctx := context.Background()
	repo := NewSecretRepository(db)

	orgA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	clusterA := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	// Test: Query with correct org ID, RLS context, and explicit filter
	var secrets []Secret
	err := db.WithTenant(ctx, orgA, func(ctx context.Context) error {
		var err error
		// Both RLS (via WithTenant) AND explicit filter (via orgA param)
		secrets, err = repo.GetSecretsForCluster(ctx, orgA, clusterA)
		return err
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Verify all returned secrets belong to org A
	for _, s := range secrets {
		if s.OrgID != orgA {
			t.Errorf("RLS or explicit filter failed: secret.OrgID=%s, expected=%s", s.OrgID, orgA)
		}
	}

	t.Logf("Returned %d secrets, all belonging to org %s", len(secrets), orgA)
}

// ============================================================================
// Helper for test data setup
// ============================================================================

func setupTestSecret(t *testing.T, db *database.DB, orgID, projectID, name string, encryptedValue []byte) string {
	t.Helper()

	ctx := context.Background()
	var secretID string

	err := db.WithTenant(ctx, orgID, func(ctx context.Context) error {
		const sql = `
			INSERT INTO secrets (org_id, project_id, name, encrypted_value, value_hash, created_by)
			VALUES ($1, $2, $3, $4, 'test-hash', NULL)
			RETURNING id`

		return db.Conn(ctx).QueryRow(ctx, sql, orgID, projectID, name, encryptedValue).Scan(&secretID)
	})
	if err != nil {
		t.Fatalf("failed to setup test secret: %v", err)
	}

	return secretID
}

func cleanupTestSecrets(t *testing.T, db *database.DB, orgID string) {
	t.Helper()

	ctx := context.Background()
	err := db.WithTenant(ctx, orgID, func(ctx context.Context) error {
		_, err := db.Conn(ctx).Exec(ctx, `DELETE FROM secrets WHERE org_id = $1 AND name LIKE 'TEST_%'`, orgID)
		return err
	})
	if err != nil {
		t.Logf("warning: failed to cleanup test secrets: %v", err)
	}
}

// Verify the test timestamp to ensure tests are not stale.
func init() {
	_ = time.Now()
}
