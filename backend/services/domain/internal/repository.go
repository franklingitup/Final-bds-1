package domain

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// TenantRunner runs a function within a tenant-scoped transaction.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// DomainStore persists domains.
type DomainStore interface {
	Create(ctx context.Context, d *Domain) error
	GetByID(ctx context.Context, id string) (*Domain, error)
	GetByFullDomain(ctx context.Context, fullDomain string) (*Domain, error)
	List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[Domain], error)
	ListByDeployment(ctx context.Context, deploymentID string, req database.PageRequest) (database.Page[Domain], error)
	ListPendingVerification(ctx context.Context) ([]Domain, error)
	Update(ctx context.Context, d *Domain) error
	UpdateVerification(ctx context.Context, id, status string, errMsg *string) error
	Delete(ctx context.Context, id string) error
}

// CertificateStore persists certificates.
type CertificateStore interface {
	Create(ctx context.Context, c *Certificate) error
	GetByID(ctx context.Context, id string) (*Certificate, error)
	GetByDomainID(ctx context.Context, domainID string) (*Certificate, error)
	ListExpiring(ctx context.Context, withinDays int) ([]Certificate, error)
	Update(ctx context.Context, c *Certificate) error
	UpdateStatus(ctx context.Context, id, status string, errMsg *string) error
	Delete(ctx context.Context, id string) error
}

// ACMEChallengeStore persists ACME challenges.
type ACMEChallengeStore interface {
	Create(ctx context.Context, c *ACMEChallenge) error
	GetByToken(ctx context.Context, token string) (*ACMEChallenge, error)
	GetByDomainID(ctx context.Context, domainID string) (*ACMEChallenge, error)
	UpdateStatus(ctx context.Context, id, status string) error
	DeleteExpired(ctx context.Context) error
}

// IngressStore persists ingress records.
type IngressStore interface {
	Create(ctx context.Context, i *IngressRecord) error
	GetByID(ctx context.Context, id string) (*IngressRecord, error)
	GetByDomainID(ctx context.Context, domainID string) (*IngressRecord, error)
	ListByCluster(ctx context.Context, clusterID string) ([]IngressRecord, error)
	ListPending(ctx context.Context, clusterID string) ([]IngressRecord, error)
	Update(ctx context.Context, i *IngressRecord) error
	UpdateSyncStatus(ctx context.Context, id, status string, observedGen int64, errMsg *string) error
	Delete(ctx context.Context, id string) error
}

// DomainEventStore persists domain events.
type DomainEventStore interface {
	Create(ctx context.Context, e *DomainEvent) error
	List(ctx context.Context, domainID string, req database.PageRequest) (database.Page[DomainEvent], error)
}

// ----------------------------------------------------------------------------
// Domain Repository
// ----------------------------------------------------------------------------

type domainRepo struct{ db *database.DB }

func NewDomainStore(db *database.DB) DomainStore { return &domainRepo{db: db} }

func (r *domainRepo) Create(ctx context.Context, d *Domain) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if d.VerificationStatus == "" {
		d.VerificationStatus = VerificationPending
	}
	if d.VerificationMethod == "" {
		d.VerificationMethod = VerifyDNSTXT
	}
	if d.Status == "" {
		d.Status = StatusPending
	}

	const sql = `
INSERT INTO domains (id, org_id, deployment_id, domain, subdomain, full_domain,
    verification_status, verification_token, verification_method,
    dns_txt_name, dns_txt_value, dns_cname_target, status, is_primary, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING created_at, updated_at, version`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		d.ID, d.OrgID, d.DeploymentID, d.Domain, d.Subdomain, d.FullDomain,
		d.VerificationStatus, d.VerificationToken, d.VerificationMethod,
		d.DNSTxtName, d.DNSTxtValue, d.DNSCnameTarget, d.Status, d.IsPrimary, d.CreatedBy)
	return database.MapError(row.Scan(&d.CreatedAt, &d.UpdatedAt, &d.Version))
}

func (r *domainRepo) GetByID(ctx context.Context, id string) (*Domain, error) {
	d, err := database.QueryOne[Domain](ctx, r.db.Conn(ctx),
		"SELECT * FROM domains WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *domainRepo) GetByFullDomain(ctx context.Context, fullDomain string) (*Domain, error) {
	d, err := database.QueryOne[Domain](ctx, r.db.Conn(ctx),
		"SELECT * FROM domains WHERE full_domain = $1", fullDomain)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *domainRepo) List(ctx context.Context, orgID string, req database.PageRequest) (database.Page[Domain], error) {
	req = req.Normalize()
	cur, err := database.DecodeCursor(req.Cursor)
	if err != nil {
		return database.Page[Domain]{}, err
	}

	var sql string
	var args []any

	if cur.IsZero() {
		sql = "SELECT * FROM domains WHERE org_id = $1 AND status != 'deleted' ORDER BY created_at DESC, id DESC LIMIT $2"
		args = []any{orgID, req.Limit + 1}
	} else {
		sql = "SELECT * FROM domains WHERE org_id = $1 AND status != 'deleted' AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT $4"
		args = []any{orgID, cur.CreatedAt, cur.ID, req.Limit + 1}
	}

	items, err := database.QueryAll[Domain](ctx, r.db.Conn(ctx), sql, args...)
	if err != nil {
		return database.Page[Domain]{}, err
	}
	return database.BuildPage(items, req.Limit, func(d Domain) database.Cursor { return d.Cursor() }), nil
}

func (r *domainRepo) ListByDeployment(ctx context.Context, deploymentID string, req database.PageRequest) (database.Page[Domain], error) {
	req = req.Normalize()
	items, err := database.QueryAll[Domain](ctx, r.db.Conn(ctx),
		"SELECT * FROM domains WHERE deployment_id = $1 AND status != 'deleted' ORDER BY is_primary DESC, created_at DESC LIMIT $2",
		deploymentID, req.Limit+1)
	if err != nil {
		return database.Page[Domain]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[Domain]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[Domain]{Items: items}, nil
}

func (r *domainRepo) ListPendingVerification(ctx context.Context) ([]Domain, error) {
	return database.QueryAll[Domain](ctx, r.db.Conn(ctx),
		`SELECT * FROM domains WHERE verification_status IN ('pending', 'verifying') 
		 AND status != 'deleted' ORDER BY created_at ASC LIMIT 100`)
}

func (r *domainRepo) Update(ctx context.Context, d *Domain) error {
	const sql = `
UPDATE domains
SET domain = $1, subdomain = $2, full_domain = $3, verification_status = $4,
    verification_error = $5, verified_at = $6, last_check_at = $7,
    status = $8, is_primary = $9, version = version + 1, updated_at = now()
WHERE id = $10 AND version = $11`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		d.Domain, d.Subdomain, d.FullDomain, d.VerificationStatus,
		d.VerificationError, d.VerifiedAt, d.LastCheckAt,
		d.Status, d.IsPrimary, d.ID, d.Version)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return database.ErrOptimisticLock
	}
	d.Version++
	return nil
}

func (r *domainRepo) UpdateVerification(ctx context.Context, id, status string, errMsg *string) error {
	var sql string
	var args []any

	if status == VerificationVerified {
		sql = `UPDATE domains SET verification_status = $1, verification_error = NULL, 
               verified_at = now(), last_check_at = now(), status = 'active', 
               version = version + 1, updated_at = now() WHERE id = $2`
		args = []any{status, id}
	} else {
		sql = `UPDATE domains SET verification_status = $1, verification_error = $2, 
               last_check_at = now(), version = version + 1, updated_at = now() WHERE id = $3`
		args = []any{status, errMsg, id}
	}

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("domain not found")
	}
	return nil
}

func (r *domainRepo) Delete(ctx context.Context, id string) error {
	const sql = `UPDATE domains SET status = 'deleted', version = version + 1, updated_at = now() WHERE id = $1`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("domain not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Certificate Repository
// ----------------------------------------------------------------------------

type certificateRepo struct{ db *database.DB }

func NewCertificateStore(db *database.DB) CertificateStore { return &certificateRepo{db: db} }

func (r *certificateRepo) Create(ctx context.Context, c *Certificate) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.Status == "" {
		c.Status = CertPending
	}
	if c.Issuer == "" {
		c.Issuer = "letsencrypt"
	}
	if len(c.SANDomains) == 0 {
		c.SANDomains = []byte("[]")
	}

	const sql = `
INSERT INTO certificates (id, org_id, domain_id, common_name, san_domains, issuer, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (domain_id) DO UPDATE SET
    common_name = EXCLUDED.common_name,
    san_domains = EXCLUDED.san_domains,
    status = CASE WHEN certificates.status = 'active' THEN certificates.status ELSE EXCLUDED.status END,
    updated_at = now()
RETURNING created_at, updated_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		c.ID, c.OrgID, c.DomainID, c.CommonName, c.SANDomains, c.Issuer, c.Status)
	return database.MapError(row.Scan(&c.CreatedAt, &c.UpdatedAt))
}

func (r *certificateRepo) GetByID(ctx context.Context, id string) (*Certificate, error) {
	c, err := database.QueryOne[Certificate](ctx, r.db.Conn(ctx),
		"SELECT * FROM certificates WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *certificateRepo) GetByDomainID(ctx context.Context, domainID string) (*Certificate, error) {
	c, err := database.QueryOne[Certificate](ctx, r.db.Conn(ctx),
		"SELECT * FROM certificates WHERE domain_id = $1", domainID)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *certificateRepo) ListExpiring(ctx context.Context, withinDays int) ([]Certificate, error) {
	return database.QueryAll[Certificate](ctx, r.db.Conn(ctx),
		`SELECT * FROM certificates 
		 WHERE status = 'active' AND expires_at < (now() + ($1 || ' days')::interval)
		 ORDER BY expires_at ASC`, withinDays)
}

func (r *certificateRepo) Update(ctx context.Context, c *Certificate) error {
	const sql = `
UPDATE certificates
SET certificate_pem = $1, private_key_pem = $2, issued_at = $3, expires_at = $4,
    status = $5, last_renewal_at = $6, renewal_error = $7, 
    acme_order_url = $8, acme_cert_url = $9, updated_at = now()
WHERE id = $10`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		c.CertificatePEM, c.PrivateKeyPEM, c.IssuedAt, c.ExpiresAt,
		c.Status, c.LastRenewalAt, c.RenewalError,
		c.ACMEOrderURL, c.ACMECertURL, c.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("certificate not found")
	}
	return nil
}

func (r *certificateRepo) UpdateStatus(ctx context.Context, id, status string, errMsg *string) error {
	const sql = `UPDATE certificates SET status = $1, renewal_error = $2, updated_at = now() WHERE id = $3`
	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, errMsg, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("certificate not found")
	}
	return nil
}

func (r *certificateRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM certificates WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("certificate not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// ACME Challenge Repository
// ----------------------------------------------------------------------------

type acmeChallengeRepo struct{ db *database.DB }

func NewACMEChallengeStore(db *database.DB) ACMEChallengeStore { return &acmeChallengeRepo{db: db} }

func (r *acmeChallengeRepo) Create(ctx context.Context, c *ACMEChallenge) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.ChallengeType == "" {
		c.ChallengeType = ChallengeHTTP01
	}
	if c.Status == "" {
		c.Status = "pending"
	}

	const sql = `
INSERT INTO acme_challenges (id, org_id, domain_id, challenge_type, token, key_auth, status, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		c.ID, c.OrgID, c.DomainID, c.ChallengeType, c.Token, c.KeyAuth, c.Status, c.ExpiresAt)
	return database.MapError(row.Scan(&c.CreatedAt))
}

func (r *acmeChallengeRepo) GetByToken(ctx context.Context, token string) (*ACMEChallenge, error) {
	c, err := database.QueryOne[ACMEChallenge](ctx, r.db.Conn(ctx),
		"SELECT * FROM acme_challenges WHERE token = $1 AND expires_at > now()", token)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *acmeChallengeRepo) GetByDomainID(ctx context.Context, domainID string) (*ACMEChallenge, error) {
	c, err := database.QueryOne[ACMEChallenge](ctx, r.db.Conn(ctx),
		"SELECT * FROM acme_challenges WHERE domain_id = $1 AND expires_at > now() ORDER BY created_at DESC LIMIT 1", domainID)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *acmeChallengeRepo) UpdateStatus(ctx context.Context, id, status string) error {
	sql := `UPDATE acme_challenges SET status = $1, validated_at = CASE WHEN $1 = 'valid' THEN now() ELSE validated_at END WHERE id = $2`
	_, err := r.db.Conn(ctx).Exec(ctx, sql, status, id)
	return database.MapError(err)
}

func (r *acmeChallengeRepo) DeleteExpired(ctx context.Context) error {
	_, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM acme_challenges WHERE expires_at < now()")
	return database.MapError(err)
}

// ----------------------------------------------------------------------------
// Ingress Repository
// ----------------------------------------------------------------------------

type ingressRepo struct{ db *database.DB }

func NewIngressStore(db *database.DB) IngressStore { return &ingressRepo{db: db} }

func (r *ingressRepo) Create(ctx context.Context, i *IngressRecord) error {
	if i.ID == "" {
		i.ID = uuid.NewString()
	}
	if i.IngressClass == "" {
		i.IngressClass = "nginx"
	}
	if i.Path == "" {
		i.Path = "/"
	}
	if i.PathType == "" {
		i.PathType = "Prefix"
	}
	if i.Status == "" {
		i.Status = IngressPending
	}

	const sql = `
INSERT INTO ingress_records (id, org_id, domain_id, cluster_id, ingress_name, namespace,
    ingress_class, service_name, service_port, path, path_type, tls_secret_name, status, generation)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (domain_id, cluster_id) DO UPDATE SET
    ingress_name = EXCLUDED.ingress_name,
    namespace = EXCLUDED.namespace,
    service_name = EXCLUDED.service_name,
    service_port = EXCLUDED.service_port,
    path = EXCLUDED.path,
    tls_secret_name = EXCLUDED.tls_secret_name,
    status = 'pending',
    generation = ingress_records.generation + 1,
    updated_at = now()
RETURNING id, created_at, updated_at, generation`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		i.ID, i.OrgID, i.DomainID, i.ClusterID, i.IngressName, i.Namespace,
		i.IngressClass, i.ServiceName, i.ServicePort, i.Path, i.PathType,
		i.TLSSecretName, i.Status, i.Generation)
	return database.MapError(row.Scan(&i.ID, &i.CreatedAt, &i.UpdatedAt, &i.Generation))
}

func (r *ingressRepo) GetByID(ctx context.Context, id string) (*IngressRecord, error) {
	i, err := database.QueryOne[IngressRecord](ctx, r.db.Conn(ctx),
		"SELECT * FROM ingress_records WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *ingressRepo) GetByDomainID(ctx context.Context, domainID string) (*IngressRecord, error) {
	i, err := database.QueryOne[IngressRecord](ctx, r.db.Conn(ctx),
		"SELECT * FROM ingress_records WHERE domain_id = $1", domainID)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *ingressRepo) ListByCluster(ctx context.Context, clusterID string) ([]IngressRecord, error) {
	return database.QueryAll[IngressRecord](ctx, r.db.Conn(ctx),
		"SELECT * FROM ingress_records WHERE cluster_id = $1 ORDER BY created_at DESC", clusterID)
}

func (r *ingressRepo) ListPending(ctx context.Context, clusterID string) ([]IngressRecord, error) {
	return database.QueryAll[IngressRecord](ctx, r.db.Conn(ctx),
		`SELECT * FROM ingress_records 
		 WHERE cluster_id = $1 AND (status = 'pending' OR generation > observed_generation)
		 ORDER BY updated_at ASC`, clusterID)
}

func (r *ingressRepo) Update(ctx context.Context, i *IngressRecord) error {
	const sql = `
UPDATE ingress_records
SET ingress_name = $1, namespace = $2, service_name = $3, service_port = $4,
    path = $5, path_type = $6, tls_secret_name = $7, manifest_hash = $8,
    status = 'pending', generation = generation + 1, updated_at = now()
WHERE id = $9`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql,
		i.IngressName, i.Namespace, i.ServiceName, i.ServicePort,
		i.Path, i.PathType, i.TLSSecretName, i.ManifestHash, i.ID)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("ingress record not found")
	}
	return nil
}

func (r *ingressRepo) UpdateSyncStatus(ctx context.Context, id, status string, observedGen int64, errMsg *string) error {
	const sql = `
UPDATE ingress_records
SET status = $1, observed_generation = $2, last_synced_at = now(), sync_error = $3, updated_at = now()
WHERE id = $4`

	tag, err := r.db.Conn(ctx).Exec(ctx, sql, status, observedGen, errMsg, id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("ingress record not found")
	}
	return nil
}

func (r *ingressRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Conn(ctx).Exec(ctx, "DELETE FROM ingress_records WHERE id = $1", id)
	if err != nil {
		return database.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("ingress record not found")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Domain Event Repository
// ----------------------------------------------------------------------------

type domainEventRepo struct{ db *database.DB }

func NewDomainEventStore(db *database.DB) DomainEventStore { return &domainEventRepo{db: db} }

func (r *domainEventRepo) Create(ctx context.Context, e *DomainEvent) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if len(e.Details) == 0 {
		e.Details = []byte("{}")
	}

	const sql = `
INSERT INTO domain_events (id, org_id, domain_id, event_type, message, details, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING created_at`

	row := r.db.Conn(ctx).QueryRow(ctx, sql,
		e.ID, e.OrgID, e.DomainID, e.EventType, e.Message, e.Details, e.CreatedBy)
	return database.MapError(row.Scan(&e.CreatedAt))
}

func (r *domainEventRepo) List(ctx context.Context, domainID string, req database.PageRequest) (database.Page[DomainEvent], error) {
	req = req.Normalize()
	items, err := database.QueryAll[DomainEvent](ctx, r.db.Conn(ctx),
		"SELECT * FROM domain_events WHERE domain_id = $1 ORDER BY created_at DESC LIMIT $2",
		domainID, req.Limit+1)
	if err != nil {
		return database.Page[DomainEvent]{}, err
	}

	if len(items) > req.Limit {
		return database.Page[DomainEvent]{
			Items:      items[:req.Limit],
			NextCursor: items[req.Limit-1].ID,
		}, nil
	}
	return database.Page[DomainEvent]{Items: items}, nil
}

// Helper to marshal details to JSON
func marshalDetails(v any) json.RawMessage {
	if v == nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(v)
	return b
}
