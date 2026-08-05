package secrets

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// Secret name validation: alphanumeric, underscores, and hyphens only.
var secretNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,254}$`)

// TenantRunner executes operations within tenant context.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// MemberLookup checks project membership.
type MemberLookup interface {
	GetByUser(ctx context.Context, projectID, userID string) (role authz.ProjectRole, err error)
}

// Deps are the secrets service dependencies.
type Deps struct {
	Secrets     SecretStore
	AccessLogs  AccessLogStore
	Encryptor   *Encryptor
	Outbox      events.Outbox
	Tenant      TenantRunner
	Members     MemberLookup
	Authorizer  authz.Authorizer
	Logger      *slog.Logger
	Now         func() time.Time
}

// Service implements the secrets domain logic.
type Service struct {
	secrets    SecretStore
	accessLogs AccessLogStore
	encryptor  *Encryptor
	outbox     events.Outbox
	tenant     TenantRunner
	members    MemberLookup
	authorizer authz.Authorizer
	log        *slog.Logger
	now        func() time.Time
}

// NewService creates a new secrets service.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Authorizer == nil {
		d.Authorizer = authz.NewPolicyAuthorizer()
	}
	return &Service{
		secrets:    d.Secrets,
		accessLogs: d.AccessLogs,
		encryptor:  d.Encryptor,
		outbox:     d.Outbox,
		tenant:     d.Tenant,
		members:    d.Members,
		authorizer: d.Authorizer,
		log:        d.Logger,
		now:        d.Now,
	}
}

// ----------------------------------------------------------------------------
// Secret Operations
// ----------------------------------------------------------------------------

// CreateSecret creates a new secret with encrypted value.
// Requires: secrets:write permission.
func (s *Service) CreateSecret(ctx context.Context, orgID, userID, projectID string, req CreateSecretRequest) (*Secret, error) {
	// Validate request
	if err := s.validateSecretName(req.Name); err != nil {
		return nil, err
	}
	if err := s.validateSecretValue(req.Value); err != nil {
		return nil, err
	}

	// Authorize: requires write permission
	if err := s.authorize(ctx, orgID, userID, projectID, authz.ActionWriteSecrets); err != nil {
		return nil, err
	}

	// Encrypt the value
	encrypted, err := s.encryptor.EncryptString(req.Value)
	if err != nil {
		s.log.ErrorContext(ctx, "failed to encrypt secret",
			slog.String("error", err.Error()),
			slog.String("project_id", projectID),
		)
		return nil, apperrors.Internal("failed to encrypt secret")
	}

	secret := &Secret{
		ProjectID:      projectID,
		Name:           req.Name,
		EncryptedValue: encrypted,
		ValueHash:      HashValue(req.Value),
		CreatedBy:      &userID,
	}
	secret.OrgID = orgID
	if req.Description != nil {
		d := strings.TrimSpace(*req.Description)
		secret.Description = &d
	}

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Check for duplicate name
		existing, err := s.secrets.GetByName(ctx, projectID, req.Name)
		if err == nil && existing != nil {
			return apperrors.Conflict("secret with name '" + req.Name + "' already exists")
		}
		if err != nil && !database.IsNotFound(err) {
			return err
		}

		// Create secret
		if err := s.secrets.Create(ctx, secret); err != nil {
			return err
		}

		// Log access
		s.logAccess(ctx, secret.OrgID, secret.ID, ActionCreated, &userID)

		// Emit event (NEVER include plaintext value)
		return s.enqueue(ctx, EventSecretCreated, orgID, secretCreatedPayload{
			SecretID:  secret.ID,
			ProjectID: projectID,
			Name:      secret.Name,
			Version:   secret.Version,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "secret", ID: secret.ID}))
	})
	if err != nil {
		return nil, err
	}

	s.log.InfoContext(ctx, "secret created",
		slog.String("secret_id", secret.ID),
		slog.String("project_id", projectID),
		slog.String("name", secret.Name),
	)

	return secret, nil
}

// GetSecret retrieves a secret by ID (without decrypting the value).
// Requires: secrets:read permission.
func (s *Service) GetSecret(ctx context.Context, orgID, userID, projectID, secretID string) (*Secret, error) {
	if err := s.authorize(ctx, orgID, userID, projectID, authz.ActionReadSecrets); err != nil {
		return nil, err
	}

	var secret *Secret
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		secret, err = s.secrets.GetByID(ctx, secretID)
		if err != nil {
			return err
		}
		// Verify secret belongs to the project
		if secret.ProjectID != projectID {
			return apperrors.NotFound("secret not found")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return secret, nil
}

// ListSecrets returns a paginated list of secrets (without decrypting values).
// Requires: secrets:read permission.
func (s *Service) ListSecrets(ctx context.Context, orgID, userID, projectID string, page database.PageRequest) (database.Page[Secret], error) {
	if err := s.authorize(ctx, orgID, userID, projectID, authz.ActionReadSecrets); err != nil {
		return database.Page[Secret]{}, err
	}

	var result database.Page[Secret]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		result, err = s.secrets.List(ctx, projectID, page)
		return err
	})

	return result, err
}

// UpdateSecret updates a secret's value and/or description.
// Requires: secrets:write permission.
func (s *Service) UpdateSecret(ctx context.Context, orgID, userID, projectID, secretID string, req UpdateSecretRequest) (*Secret, error) {
	if err := s.authorize(ctx, orgID, userID, projectID, authz.ActionWriteSecrets); err != nil {
		return nil, err
	}

	if req.Value != nil {
		if err := s.validateSecretValue(*req.Value); err != nil {
			return nil, err
		}
	}

	var secret *Secret
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		secret, err = s.secrets.GetByID(ctx, secretID)
		if err != nil {
			return err
		}
		// Verify secret belongs to the project
		if secret.ProjectID != projectID {
			return apperrors.NotFound("secret not found")
		}

		// Update fields
		if req.Description != nil {
			d := strings.TrimSpace(*req.Description)
			secret.Description = &d
		}

		if req.Value != nil {
			// Encrypt new value
			encrypted, err := s.encryptor.EncryptString(*req.Value)
			if err != nil {
				s.log.ErrorContext(ctx, "failed to encrypt secret",
					slog.String("error", err.Error()),
					slog.String("secret_id", secretID),
				)
				return apperrors.Internal("failed to encrypt secret")
			}
			secret.EncryptedValue = encrypted
			secret.ValueHash = HashValue(*req.Value)
		}

		secret.UpdatedBy = &userID

		if err := s.secrets.Update(ctx, secret); err != nil {
			return err
		}

		// Log access
		s.logAccess(ctx, secret.OrgID, secret.ID, ActionUpdated, &userID)

		// Emit event (NEVER include plaintext value)
		return s.enqueue(ctx, EventSecretUpdated, orgID, secretUpdatedPayload{
			SecretID:  secret.ID,
			ProjectID: projectID,
			Name:      secret.Name,
			Version:   secret.Version,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "secret", ID: secret.ID}))
	})
	if err != nil {
		return nil, err
	}

	s.log.InfoContext(ctx, "secret updated",
		slog.String("secret_id", secret.ID),
		slog.String("project_id", projectID),
		slog.Int64("version", secret.Version),
	)

	return secret, nil
}

// DeleteSecret soft-deletes a secret.
// Requires: secrets:manage permission.
func (s *Service) DeleteSecret(ctx context.Context, orgID, userID, projectID, secretID string) error {
	if err := s.authorize(ctx, orgID, userID, projectID, authz.ActionManageSecrets); err != nil {
		return err
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		secret, err := s.secrets.GetByID(ctx, secretID)
		if err != nil {
			return err
		}
		// Verify secret belongs to the project
		if secret.ProjectID != projectID {
			return apperrors.NotFound("secret not found")
		}

		if err := s.secrets.Delete(ctx, secretID); err != nil {
			return err
		}

		// Log access
		s.logAccess(ctx, secret.OrgID, secret.ID, ActionDeleted, &userID)

		// Emit event
		return s.enqueue(ctx, EventSecretDeleted, orgID, secretDeletedPayload{
			SecretID:  secretID,
			ProjectID: projectID,
			Name:      secret.Name,
			DeletedBy: userID,
		}, events.WithActor(events.Actor{Type: "user", ID: userID}),
			events.WithResource(events.Resource{Type: "secret", ID: secretID}))
	})
	if err != nil {
		return err
	}

	s.log.InfoContext(ctx, "secret deleted",
		slog.String("secret_id", secretID),
		slog.String("project_id", projectID),
		slog.String("deleted_by", userID),
	)

	return nil
}

// ----------------------------------------------------------------------------
// Agent API
// ----------------------------------------------------------------------------

// GetSecretsForCluster retrieves all secrets for deployments on a cluster.
// This is called by the agent sync endpoint after agent authentication.
//
// SECURITY (CRIT-001 FIX):
// - Caller must validate cluster ownership before calling this.
// - orgID must be the authenticated agent's organization ID.
// - Query executes within tenant context (RLS) AND with explicit org_id filter.
// - Both protections coexist for defense-in-depth.
func (s *Service) GetSecretsForCluster(ctx context.Context, orgID, clusterID string) ([]AgentSecret, error) {
	// SECURITY: Validate orgID is provided
	if orgID == "" {
		s.log.ErrorContext(ctx, "GetSecretsForCluster called without orgID",
			slog.String("cluster_id", clusterID),
		)
		return nil, ErrInvalidOrgID
	}

	var secrets []Secret
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		// SECURITY: Pass orgID for explicit filtering (defense-in-depth)
		secrets, err = s.secrets.GetSecretsForCluster(ctx, orgID, clusterID)
		return err
	})
	if err != nil {
		return nil, err
	}

	// Decrypt secrets for the agent
	result := make([]AgentSecret, 0, len(secrets))
	for _, sec := range secrets {
		value, err := s.encryptor.DecryptString(sec.EncryptedValue)
		if err != nil {
			s.log.ErrorContext(ctx, "failed to decrypt secret for agent",
				slog.String("error", err.Error()),
				slog.String("secret_id", sec.ID),
			)
			continue // Skip secrets that can't be decrypted
		}

		result = append(result, AgentSecret{
			ProjectID: sec.ProjectID,
			Name:      sec.Name,
			Value:     value,
			Version:   sec.Version,
		})
	}

	s.log.InfoContext(ctx, "agent retrieved secrets",
		slog.String("cluster_id", clusterID),
		slog.Int("secret_count", len(result)),
	)

	return result, nil
}

// ----------------------------------------------------------------------------
// Internal Helpers
// ----------------------------------------------------------------------------

func (s *Service) authorize(ctx context.Context, orgID, userID, projectID string, action authz.Action) error {
	var projectRole authz.ProjectRole
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		projectRole, err = s.members.GetByUser(ctx, projectID, userID)
		return err
	})
	if err != nil {
		if database.IsNotFound(err) {
			return apperrors.Forbidden("not a project member")
		}
		return err
	}

	principal := authz.Principal{
		UserID:       userID,
		OrgID:        orgID,
		ProjectRoles: map[string]authz.ProjectRole{projectID: projectRole},
	}
	return s.authorizer.Authorize(ctx, principal, authz.AccessRequest{
		Action:    action,
		OrgID:     orgID,
		ProjectID: projectID,
	})
}

func (s *Service) validateSecretName(name string) error {
	if !secretNamePattern.MatchString(name) {
		return apperrors.Validation(
			"secret name must start with uppercase letter and contain only uppercase letters, digits, and underscores (max 255 chars)",
		)
	}
	return nil
}

func (s *Service) validateSecretValue(value string) error {
	if len(value) == 0 {
		return apperrors.Validation("secret value cannot be empty")
	}
	if len(value) > 65536 { // 64KB max
		return apperrors.Validation("secret value cannot exceed 64KB")
	}
	return nil
}

func (s *Service) logAccess(ctx context.Context, orgID, secretID, action string, userID *string) {
	log := &SecretAccessLog{
		OrgID:       orgID,
		SecretID:    secretID,
		Action:      action,
		PerformedBy: userID,
	}
	if s.accessLogs != nil {
		if err := s.accessLogs.Create(ctx, log); err != nil {
			s.log.ErrorContext(ctx, "failed to log secret access",
				slog.String("error", err.Error()),
				slog.String("secret_id", secretID),
			)
		}
	}
}
