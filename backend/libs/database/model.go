package database

import "time"

// Model holds the columns shared by every platform entity. Embed it in concrete
// row structs; the `db` tags are consumed by pgx's named row scanners.
//
// Version backs optimistic concurrency: every UPDATE bumps it and guards on its
// prior value (see Repository.UpdateVersioned).
type Model struct {
	ID        string    `db:"id" json:"id"`
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
	Version   int64     `db:"version" json:"version"`
}

// TenantModel embeds Model and adds the tenant discriminator required on every
// tenant-owned table. RLS policies key on org_id.
type TenantModel struct {
	Model
	OrgID string `db:"org_id" json:"orgId"`
}

// Cursor implements an entity's keyset position for pagination.
func (m Model) Cursor() Cursor { return Cursor{CreatedAt: m.CreatedAt, ID: m.ID} }
