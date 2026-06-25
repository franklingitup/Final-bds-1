package database

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Migration is one versioned schema change, loaded from a paired
// `<version>_<name>.up.sql` / `.down.sql` file set.
type Migration struct {
	Version int64
	Name    string
	Up      string
	Down    string
}

// migrationFile matches golang-migrate style names, e.g. "0001_init.up.sql".
var migrationFile = regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)

// safeIdentifier guards the migrations table name, which is interpolated into
// SQL. Only per-service table names we generate are expected here.
var safeIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// LoadMigrations reads and pairs migration files from fsys (e.g. an embed.FS or
// os.DirFS). Files must be named `<version>_<name>.<up|down>.sql`. Each version
// must have both an up and a down file. The result is sorted ascending.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("database: read migrations dir: %w", err)
	}

	byVersion := map[int64]*Migration{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		m := migrationFile.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		version, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("database: bad migration version %q: %w", entry.Name(), err)
		}
		body, err := fs.ReadFile(fsys, path.Join(".", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("database: read %s: %w", entry.Name(), err)
		}

		mig := byVersion[version]
		if mig == nil {
			mig = &Migration{Version: version, Name: m[2]}
			byVersion[version] = mig
		}
		if m[3] == "up" {
			mig.Up = string(body)
		} else {
			mig.Down = string(body)
		}
	}

	out := make([]Migration, 0, len(byVersion))
	for _, mig := range byVersion {
		if mig.Up == "" {
			return nil, fmt.Errorf("database: migration %d (%s) is missing its .up.sql", mig.Version, mig.Name)
		}
		out = append(out, *mig)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Migrator applies ordered SQL migrations against a database, recording applied
// versions in a per-service tracking table. Each migration runs in its own
// transaction so a failure leaves the schema at the last good version.
//
// The tracking table is independent from golang-migrate's, allowing multiple
// services to share one database by using distinct table names (e.g.
// "schema_migrations_tenant").
type Migrator struct {
	db         *DB
	table      string
	migrations []Migration
}

// NewMigrator builds a Migrator. table must be a valid SQL identifier.
func NewMigrator(db *DB, table string, migrations []Migration) (*Migrator, error) {
	if !safeIdentifier.MatchString(table) {
		return nil, fmt.Errorf("database: invalid migrations table name %q", table)
	}
	sorted := append([]Migration(nil), migrations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })
	return &Migrator{db: db, table: table, migrations: sorted}, nil
}

// Up applies all pending migrations in ascending order and returns how many were
// applied.
func (m *Migrator) Up(ctx context.Context) (int, error) {
	if err := m.ensureTable(ctx); err != nil {
		return 0, err
	}
	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, mig := range m.migrations {
		if applied[mig.Version] {
			continue
		}
		if err := m.runOne(ctx, mig, true); err != nil {
			return count, fmt.Errorf("database: apply migration %d (%s): %w", mig.Version, mig.Name, err)
		}
		count++
	}
	return count, nil
}

// Down rolls back the most recently applied migrations, at most steps of them.
func (m *Migrator) Down(ctx context.Context, steps int) error {
	if err := m.ensureTable(ctx); err != nil {
		return err
	}
	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return err
	}

	// Walk migrations newest-first, rolling back those that are applied.
	rolledBack := 0
	for i := len(m.migrations) - 1; i >= 0 && rolledBack < steps; i-- {
		mig := m.migrations[i]
		if !applied[mig.Version] {
			continue
		}
		if mig.Down == "" {
			return fmt.Errorf("database: migration %d (%s) has no .down.sql", mig.Version, mig.Name)
		}
		if err := m.runOne(ctx, mig, false); err != nil {
			return fmt.Errorf("database: roll back migration %d (%s): %w", mig.Version, mig.Name, err)
		}
		rolledBack++
	}
	return nil
}

// Version returns the highest applied migration version, or 0 if none.
func (m *Migrator) Version(ctx context.Context) (int64, error) {
	if err := m.ensureTable(ctx); err != nil {
		return 0, err
	}
	var version int64
	sql := fmt.Sprintf("SELECT coalesce(max(version), 0) FROM %s", m.table)
	if err := m.db.Pool.QueryRow(ctx, sql).Scan(&version); err != nil {
		return 0, MapError(err)
	}
	return version, nil
}

func (m *Migrator) ensureTable(ctx context.Context) error {
	sql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		version    BIGINT PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`, m.table)
	if _, err := m.db.Pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("database: ensure migrations table: %w", err)
	}
	return nil
}

func (m *Migrator) appliedVersions(ctx context.Context) (map[int64]bool, error) {
	sql := fmt.Sprintf("SELECT version FROM %s", m.table)
	rows, err := m.db.Pool.Query(ctx, sql)
	if err != nil {
		return nil, MapError(err)
	}
	defer rows.Close()

	applied := map[int64]bool{}
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, MapError(err)
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// runOne executes a single migration's SQL and updates the tracking table inside
// one transaction (PostgreSQL DDL is transactional).
func (m *Migrator) runOne(ctx context.Context, mig Migration, up bool) error {
	tx, err := m.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	body := mig.Up
	record := fmt.Sprintf("INSERT INTO %s (version, name) VALUES ($1, $2)", m.table)
	if !up {
		body = mig.Down
		record = fmt.Sprintf("DELETE FROM %s WHERE version = $1", m.table)
	}

	if strings.TrimSpace(body) != "" {
		if _, err := tx.Exec(ctx, body); err != nil {
			return err
		}
	}

	if up {
		if _, err := tx.Exec(ctx, record, mig.Version, mig.Name); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, record, mig.Version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
