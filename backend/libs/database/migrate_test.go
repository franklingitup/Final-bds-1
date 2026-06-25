package database

import (
	"testing"
	"testing/fstest"
)

func TestLoadMigrations_PairsAndSorts(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_add_index.up.sql":   {Data: []byte("CREATE INDEX ...;")},
		"0002_add_index.down.sql": {Data: []byte("DROP INDEX ...;")},
		"0001_init.up.sql":        {Data: []byte("CREATE TABLE ...;")},
		"0001_init.down.sql":      {Data: []byte("DROP TABLE ...;")},
		"README.md":               {Data: []byte("ignore me")},
	}

	migs, err := LoadMigrations(fsys)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(migs) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(migs))
	}
	if migs[0].Version != 1 || migs[1].Version != 2 {
		t.Errorf("migrations not sorted ascending: %d, %d", migs[0].Version, migs[1].Version)
	}
	if migs[0].Name != "init" || migs[0].Up == "" || migs[0].Down == "" {
		t.Errorf("unexpected first migration: %+v", migs[0])
	}
}

func TestLoadMigrations_MissingUp(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.down.sql": {Data: []byte("DROP TABLE ...;")},
	}
	if _, err := LoadMigrations(fsys); err == nil {
		t.Error("expected error when .up.sql is missing")
	}
}

func TestNewMigrator_RejectsUnsafeTable(t *testing.T) {
	if _, err := NewMigrator(nil, "schema migrations; DROP TABLE x", nil); err == nil {
		t.Error("expected rejection of unsafe table name")
	}
	if _, err := NewMigrator(nil, "schema_migrations_tenant", nil); err != nil {
		t.Errorf("valid table name rejected: %v", err)
	}
}
