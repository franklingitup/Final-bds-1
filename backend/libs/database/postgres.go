// Package database provides PostgreSQL (pgx) and Redis connectivity plus the
// platform's shared persistence primitives: a transaction manager with tenant
// scoping and retries, generic repository helpers, cursor pagination, audit
// recording, optimistic locking, and a self-contained SQL migration runner.
//
// Tenant isolation is enforced at two layers: PostgreSQL row-level security
// policies keyed on the `app.current_org_id` session variable (set per
// transaction by WithTenant), and application-level authorization in libs/authz.
// See docs/05-database-design.md and docs/06-security-design.md.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bdsplatform/platform/backend/libs/config"
)

// DB wraps a pgx connection pool and is the entrypoint for all persistence
// helpers in this package.
type DB struct {
	*pgxpool.Pool
}

// Connect opens and verifies a PostgreSQL connection pool from configuration.
func Connect(ctx context.Context, cfg config.Config) (*DB, error) {
	poolCfg, err := poolConfig(cfg.Database)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// poolConfig builds a pgxpool.Config from configuration. Exposed for testing.
func poolConfig(cfg config.DatabaseConfig) (*pgxpool.Config, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database: DATABASE_URL is required")
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	return poolCfg, nil
}

// Health verifies the pool can reach the database.
func (db *DB) Health(ctx context.Context) error {
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("database: unhealthy: %w", err)
	}
	return nil
}

// Conn returns the active Querier for ctx: the ambient transaction when one is
// present (so repository calls join it), otherwise the connection pool.
func (db *DB) Conn(ctx context.Context) Querier {
	if tx, ok := TxFromContext(ctx); ok {
		return tx
	}
	return db.Pool
}

// Close releases the pool. Safe to call once during graceful shutdown.
func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}
