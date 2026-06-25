# Migrations

Per-service SQL migrations. Each service owns its own tables (see
`docs/05-database-design.md`). Files follow the golang-migrate naming
convention so they work with both the `migrate` CLI and the built-in Go
runner (`libs/database.Migrator`).

```text
migrations/
  <service>/
    0001_<name>.up.sql
    0001_<name>.down.sql
```

## Applying migrations

**Built-in runner (recommended for app startup):** migrations are embedded via
`migrations.Service(<service>)` and applied with `libs/database.Migrator`. Each
migration runs in its own transaction and applied versions are tracked in a
per-service table (e.g. `schema_migrations_tenant`), so multiple services can
safely share one database.

```go
fsys, _ := migrations.Service("tenant")
migs, _ := database.LoadMigrations(fsys)
m, _ := database.NewMigrator(db, "schema_migrations_tenant", migs)
applied, err := m.Up(ctx)
```

**CLI (ad-hoc / ops):** `scripts/db/migrate.sh <service> [up|down]` shells out to
the [`migrate`](https://github.com/golang-migrate/migrate) CLI.

## Conventions

- Every row carries `id`, `created_at`, `updated_at`, and a `version` column;
  `version` backs optimistic locking (`Repository.UpdateVersioned`).
- A shared `set_updated_at()` trigger maintains `updated_at` on UPDATE.
- Every tenant-owned table includes `org_id` and enables Row-Level Security
  keyed on the `app.current_org_id` session variable set by
  `database.WithTenant`.
- Lists are indexed on `(org_id, created_at DESC, id DESC)` to match keyset
  (cursor) pagination.
- `audit_logs` and `deployment_revisions` are insert-only (trigger-protected).
- Secrets tables never contain a plaintext value column.
