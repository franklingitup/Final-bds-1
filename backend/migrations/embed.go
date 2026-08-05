// Package migrations embeds the platform's per-service SQL migrations so they
// can be applied programmatically via libs/database.Migrator without shipping
// loose files alongside binaries.
//
// Usage:
//
//	fsys, _ := migrations.Service("tenant")
//	migs, _ := database.LoadMigrations(fsys)
//	m, _ := database.NewMigrator(db, "schema_migrations_tenant", migs)
//	applied, err := m.Up(ctx)
package migrations

import (
	"embed"
	"io/fs"
)

// files embeds every service's migration directory. Add new service directories
// to this directive as they are created.
//
//go:embed auth tenant project cluster deployment audit secrets outbox build domain observability notification provisioning
var files embed.FS

// Service returns the migration file system for a single service directory,
// suitable for passing to database.LoadMigrations.
func Service(name string) (fs.FS, error) {
	return fs.Sub(files, name)
}
