package deployment

import (
	"context"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// DesiredStateStore provides read model queries for agent desired state.
type DesiredStateStore interface {
	// GetDesiredState returns all active deployments for a cluster with their latest active releases.
	// The orgID parameter ensures tenant isolation - only deployments belonging to the specified
	// organization are returned. This prevents cross-tenant data access (CRIT-001 fix).
	GetDesiredState(ctx context.Context, orgID, clusterID string) ([]DesiredDeployment, error)
}

// TenantQuerier provides tenant-scoped database operations.
type TenantQuerier interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

type desiredStateRepo struct {
	db *database.DB
}

// NewDesiredStateStore creates a new DesiredStateStore.
func NewDesiredStateStore(db *database.DB) DesiredStateStore {
	return &desiredStateRepo{db: db}
}

// GetDesiredState fetches all deployments for a cluster with application and release data joined.
// Only returns deployments with an active release (pending or deploying).
//
// SECURITY: This method enforces tenant isolation by:
// 1. Executing within a tenant-scoped transaction (RLS enforced via app.current_org_id)
// 2. Explicitly filtering by org_id as a defense-in-depth measure
//
// This addresses CRIT-001: RLS Bypass in Agent Desired State Query.
func (r *desiredStateRepo) GetDesiredState(ctx context.Context, orgID, clusterID string) ([]DesiredDeployment, error) {
	var results []DesiredDeployment

	// Execute query within tenant context to enable RLS enforcement.
	err := r.db.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// This query joins applications, deployments, and releases to get the complete
		// desired state information the agent needs.
		//
		// We use DISTINCT ON to get the latest release per deployment (by revision DESC).
		// Only active deployments (not deleted) with pending/deploying releases are returned.
		//
		// SECURITY: Explicit org_id filter provides defense-in-depth beyond RLS.
		const sql = `
SELECT DISTINCT ON (d.id)
    d.id AS deployment_id,
    r.id AS release_id,
    a.id AS application_id,
    a.name AS application_name,
    a.slug AS application_slug,
    r.image AS image,
    r.revision AS revision,
    d.replicas AS replicas,
    d.port AS port,
    d.env_vars AS env_vars,
    d.cpu_request AS cpu_request,
    d.cpu_limit AS cpu_limit,
    d.memory_request AS memory_request,
    d.memory_limit AS memory_limit,
    COALESCE(r.status, d.status) AS status
FROM deployments d
INNER JOIN applications a ON a.id = d.application_id
INNER JOIN releases r ON r.deployment_id = d.id
WHERE d.cluster_id = $1
  AND d.org_id = $2
  AND d.status NOT IN ('deleted', 'deleting')
  AND r.status IN ('pending', 'deploying', 'succeeded')
ORDER BY d.id, r.revision DESC`

		rows, err := r.db.Conn(ctx).Query(ctx, sql, clusterID, orgID)
		if err != nil {
			return database.MapError(err)
		}
		defer rows.Close()

		for rows.Next() {
			var row desiredDeploymentRow
			if err := rows.Scan(
				&row.DeploymentID,
				&row.ReleaseID,
				&row.ApplicationID,
				&row.ApplicationName,
				&row.ApplicationSlug,
				&row.Image,
				&row.Revision,
				&row.Replicas,
				&row.Port,
				&row.EnvVars,
				&row.CPURequest,
				&row.CPULimit,
				&row.MemoryRequest,
				&row.MemoryLimit,
				&row.Status,
			); err != nil {
				return database.MapError(err)
			}

			// Use the application slug as the default namespace.
			namespace := row.ApplicationSlug
			results = append(results, row.toDesiredDeployment(namespace))
		}

		return rows.Err()
	})

	if err != nil {
		return nil, err
	}

	return results, nil
}
