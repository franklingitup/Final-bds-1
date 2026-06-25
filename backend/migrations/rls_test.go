package migrations

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestForceRLS_MigrationExists verifies that FORCE ROW LEVEL SECURITY
// migrations exist for all services (SEC-CRIT-04).
func TestForceRLS_MigrationExists(t *testing.T) {
	services := []struct {
		name    string
		file    string
		tables  []string
	}{
		{
			name:   "tenant",
			file:   "tenant/0003_force_rls.up.sql",
			tables: []string{"organizations", "projects"},
		},
		{
			name:   "tenant-memberships",
			file:   "tenant/0004_force_rls_memberships.up.sql",
			tables: []string{"organization_members", "organization_invitations"},
		},
		{
			name:   "project",
			file:   "project/0002_force_rls.up.sql",
			tables: []string{"project_members"},
		},
		{
			name:   "cluster",
			file:   "cluster/0002_force_rls.up.sql",
			tables: []string{"clusters", "cluster_registration_tokens", "cluster_heartbeats"},
		},
		{
			name:   "deployment",
			file:   "deployment/0002_force_rls.up.sql",
			tables: []string{"applications", "deployments", "releases"},
		},
		{
			name:   "secrets",
			file:   "secrets/0002_force_rls.up.sql",
			tables: []string{"secrets", "secret_access_logs"},
		},
		{
			name:   "audit",
			file:   "audit/0002_force_rls.up.sql",
			tables: []string{"audit_logs"},
		},
		{
			name:   "auth",
			file:   "auth/0002_force_rls.up.sql",
			tables: []string{"service_accounts", "api_tokens"},
		},
	}

	for _, svc := range services {
		t.Run(svc.name, func(t *testing.T) {
			path := filepath.Join(".", svc.file)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read migration file %s: %v", svc.file, err)
			}

			sql := string(content)
			for _, table := range svc.tables {
				expected := "ALTER TABLE " + table + " FORCE ROW LEVEL SECURITY"
				if !strings.Contains(sql, expected) {
					t.Errorf("Migration %s missing FORCE RLS for table %s", svc.file, table)
				}
			}
		})
	}
}

// TestForceRLS_DownMigrationExists verifies that down migrations exist
// to revert FORCE RLS.
func TestForceRLS_DownMigrationExists(t *testing.T) {
	downFiles := []string{
		"tenant/0003_force_rls.down.sql",
		"tenant/0004_force_rls_memberships.down.sql",
		"project/0002_force_rls.down.sql",
		"cluster/0002_force_rls.down.sql",
		"deployment/0002_force_rls.down.sql",
		"secrets/0002_force_rls.down.sql",
		"audit/0002_force_rls.down.sql",
		"auth/0002_force_rls.down.sql",
	}

	for _, file := range downFiles {
		t.Run(file, func(t *testing.T) {
			path := filepath.Join(".", file)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read down migration file %s: %v", file, err)
			}

			if !strings.Contains(string(content), "NO FORCE ROW LEVEL SECURITY") {
				t.Errorf("Down migration %s missing NO FORCE ROW LEVEL SECURITY", file)
			}
		})
	}
}

// TestMigrationSQL_ValidSyntax verifies migration files contain valid SQL patterns.
func TestMigrationSQL_ValidSyntax(t *testing.T) {
	files, err := filepath.Glob("./*/*.up.sql")
	if err != nil {
		t.Fatalf("Failed to glob migration files: %v", err)
	}

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			f, err := os.Open(file)
			if err != nil {
				t.Fatalf("Failed to open file: %v", err)
			}
			defer f.Close()

			scanner := bufio.NewScanner(f)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				line := scanner.Text()
				
				// Check for common SQL issues
				if strings.Contains(line, ";;") && !strings.HasPrefix(strings.TrimSpace(line), "--") {
					t.Errorf("Line %d has double semicolon: %s", lineNum, line)
				}
			}
		})
	}
}
