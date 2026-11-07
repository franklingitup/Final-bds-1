package deployment

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// namespacePattern validates Kubernetes namespace names:
// lowercase alphanumeric + hyphens, 1–63 chars, start/end alphanumeric.
var namespacePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func validateNamespace(ns string) error {
	if len(ns) < 1 || len(ns) > 63 {
		return apperrors.Validation("namespace must be 1–63 characters")
	}
	if !namespacePattern.MatchString(ns) {
		return apperrors.Validation("namespace must consist of lowercase alphanumeric characters or hyphens, and start and end with an alphanumeric character")
	}
	return nil
}

// authorizeEnvironment loads an environment and checks that the caller has the
// given action on the owning application. Returns the environment on success.
func (s *Service) authorizeEnvironment(ctx context.Context, orgID, userID, envID string, action authz.Action) (*Environment, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}
	var env *Environment
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		env, err = s.envs.GetByID(ctx, envID)
		return err
	})
	if err != nil {
		return nil, errEnvironmentNotFound
	}
	if _, err := s.authorizeApplication(ctx, orgID, userID, env.ApplicationID, action); err != nil {
		return nil, err
	}
	return env, nil
}

// ----------------------------------------------------------------------------
// Environment CRUD
// ----------------------------------------------------------------------------

// CreateEnvironment creates a named deployment environment for an application.
// SECURITY: Requires ActionDeploy on the application (ProjectAdmin, ProjectDeveloper, OrgAdmin+).
func (s *Service) CreateEnvironment(ctx context.Context, orgID, userID, appID string, req CreateEnvironmentRequest) (*Environment, error) {
	if _, err := s.authorizeApplication(ctx, orgID, userID, appID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateNamespace(req.Namespace); err != nil {
		return nil, err
	}
	if req.ClusterID == "" {
		return nil, apperrors.Validation("clusterId is required")
	}

	env := &Environment{
		OrgID:         orgID,
		ApplicationID: appID,
		Name:          name,
		Namespace:     req.Namespace,
		ClusterID:     req.ClusterID,
		OrderIndex:    req.OrderIndex,
		CreatedBy:     &userID,
	}

	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if err := s.envs.Create(ctx, env); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errEnvironmentNameTaken
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return env, nil
}

// GetEnvironment returns an environment by ID.
// SECURITY: Requires ActionReadDeployment on the owning application.
func (s *Service) GetEnvironment(ctx context.Context, orgID, userID, envID string) (*Environment, error) {
	return s.authorizeEnvironment(ctx, orgID, userID, envID, authz.ActionReadDeployment)
}

// ListEnvironments returns all environments for an application, ordered by order_index.
// SECURITY: Requires ActionReadDeployment on the application.
func (s *Service) ListEnvironments(ctx context.Context, orgID, userID, appID string) ([]Environment, error) {
	if _, err := s.authorizeApplication(ctx, orgID, userID, appID, authz.ActionReadDeployment); err != nil {
		return nil, err
	}

	var items []Environment
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		items, err = s.envs.List(ctx, appID)
		return err
	})
	return items, err
}

// UpdateEnvironment updates an environment's mutable fields (name, namespace, orderIndex).
// SECURITY: Requires ActionDeploy on the owning application.
func (s *Service) UpdateEnvironment(ctx context.Context, orgID, userID, envID string, req UpdateEnvironmentRequest) (*Environment, error) {
	env, err := s.authorizeEnvironment(ctx, orgID, userID, envID, authz.ActionDeploy)
	if err != nil {
		return nil, err
	}

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if err := validateName(name); err != nil {
				return err
			}
			env.Name = name
		}
		if req.Namespace != nil {
			if err := validateNamespace(*req.Namespace); err != nil {
				return err
			}
			env.Namespace = *req.Namespace
		}
		if req.OrderIndex != nil {
			env.OrderIndex = *req.OrderIndex
		}
		if err := s.envs.Update(ctx, env); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errEnvironmentNameTaken
			}
			return err
		}
		return nil
	})
	return env, err
}

// DeleteEnvironment removes an environment.
// SECURITY: Requires ActionDeploy on the owning application.
func (s *Service) DeleteEnvironment(ctx context.Context, orgID, userID, envID string) error {
	if _, err := s.authorizeEnvironment(ctx, orgID, userID, envID, authz.ActionDeploy); err != nil {
		return err
	}
	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.envs.Delete(ctx, envID)
	})
}

// ----------------------------------------------------------------------------
// Promotion requests
// ----------------------------------------------------------------------------

// RequestPromotion creates a pending promotion request from one environment to another.
// The caller must have ActionDeploy (ProjectDeveloper or above). The request waits
// for an approver with ActionManageProject before it is applied.
// SECURITY: Requires ActionDeploy on the application.
func (s *Service) RequestPromotion(ctx context.Context, orgID, userID string, req RequestPromotionRequest) (*PromotionRequest, error) {
	if req.FromEnvironmentID == req.ToEnvironmentID {
		return nil, errSameEnvironment
	}

	var promo *PromotionRequest

	// Load from-environment to get application_id and authorize.
	var fromEnv *Environment
	var err error
	if fromEnv, err = s.authorizeEnvironment(ctx, orgID, userID, req.FromEnvironmentID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Verify to-environment exists and belongs to the same application.
		toEnv, err := s.envs.GetByID(ctx, req.ToEnvironmentID)
		if err != nil {
			return errEnvironmentNotFound
		}
		if toEnv.ApplicationID != fromEnv.ApplicationID {
			return errEnvironmentApplicationMismatch
		}

		// Verify release exists and belongs to the from-environment's application.
		rel, err := s.rels.GetByID(ctx, req.ReleaseID)
		if err != nil {
			return errReleaseNotFound
		}
		// Sanity-check: release must belong to a deployment in the from-environment.
		srcDep, err := s.deps.GetByID(ctx, rel.DeploymentID)
		if err != nil {
			return errDeploymentNotFound
		}
		if srcDep.EnvironmentID == nil || *srcDep.EnvironmentID != fromEnv.ID {
			return apperrors.Validation("release does not belong to the specified source environment")
		}

		promo = &PromotionRequest{
			OrgID:             orgID,
			ApplicationID:     fromEnv.ApplicationID,
			ReleaseID:         req.ReleaseID,
			FromEnvironmentID: req.FromEnvironmentID,
			ToEnvironmentID:   req.ToEnvironmentID,
			RequestedBy:       userID,
			Status:            PromotionStatusPending,
		}
		return s.promos.Create(ctx, promo)
	})
	return promo, err
}

// GetPromotion returns a promotion request by ID.
// SECURITY: Requires ActionReadDeployment on the owning application.
func (s *Service) GetPromotion(ctx context.Context, orgID, userID, promotionID string) (*PromotionRequest, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}
	var promo *PromotionRequest
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		promo, err = s.promos.GetByID(ctx, promotionID)
		return err
	})
	if err != nil {
		return nil, errPromotionNotFound
	}
	// Authorize via the owning application.
	if _, err := s.authorizeApplication(ctx, orgID, userID, promo.ApplicationID, authz.ActionReadDeployment); err != nil {
		return nil, err
	}
	return promo, nil
}

// ListPromotions returns all promotion requests for an application.
// SECURITY: Requires ActionReadDeployment on the application.
func (s *Service) ListPromotions(ctx context.Context, orgID, userID, appID string) ([]PromotionRequest, error) {
	if _, err := s.authorizeApplication(ctx, orgID, userID, appID, authz.ActionReadDeployment); err != nil {
		return nil, err
	}
	var items []PromotionRequest
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		items, err = s.promos.ListByApplication(ctx, appID)
		return err
	})
	return items, err
}

// ApprovePromotion approves a pending promotion and immediately applies it.
//
// What "apply" means:
//  1. Find (or create) the deployment bound to the target environment.
//  2. Create a new Release in that deployment using the source release's digest-pinned image.
//  3. Mark the PromotionRequest as applied.
//
// This reuses the internal release-creation code path directly (not the public
// CreateDeployment method) because authorization is already checked here and
// the inputs are trusted (sourced from an existing Release, not user-supplied).
//
// SECURITY: Requires ActionManageProject (ProjectAdmin, OrgAdmin, OrgOwner).
// ProjectDeveloper can request but NOT approve promotions.
func (s *Service) ApprovePromotion(ctx context.Context, orgID, userID, promotionID string) (*PromotionRequest, *Deployment, *Release, error) {
	// Load the promotion first (org-read check).
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, nil, nil, err
	}

	var promo *PromotionRequest
	var fromEnv *Environment
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		promo, err = s.promos.GetByID(ctx, promotionID)
		if err != nil {
			return errPromotionNotFound
		}
		if promo.Status != PromotionStatusPending {
			return errPromotionNotPending
		}
		fromEnv, err = s.envs.GetByID(ctx, promo.FromEnvironmentID)
		return err
	})
	if err != nil {
		return nil, nil, nil, err
	}

	// SECURITY: approval requires ActionManageProject — stricter than ActionDeploy.
	// ProjectDeveloper has ActionDeploy but NOT ActionManageProject, so they can
	// request promotions but cannot approve their own requests.
	if _, err := s.authorizeApplication(ctx, orgID, userID, fromEnv.ApplicationID, authz.ActionManageProject); err != nil {
		return nil, nil, nil, err
	}

	var dep *Deployment
	var rel *Release
	var updatedPromo *PromotionRequest

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		toEnv, err := s.envs.GetByID(ctx, promo.ToEnvironmentID)
		if err != nil {
			return errEnvironmentNotFound
		}

		// Gap 1 fix: re-verify the cross-application invariant at apply time.
		// The FK RESTRICT on from/to_environment_id prevents deletion of either
		// environment while the PromotionRequest exists, so this is a defence-in-depth
		// check against any future code path that could reassign an environment to a
		// different application.
		if toEnv.ApplicationID != fromEnv.ApplicationID {
			return errEnvironmentApplicationMismatch
		}

		srcRelease, err := s.rels.GetByID(ctx, promo.ReleaseID)
		if err != nil {
			return errReleaseNotFound
		}

		// Find or create a deployment for the target environment.
		dep, err = s.deps.GetLatestByEnvironment(ctx, toEnv.ID)
		if err != nil {
			if !database.IsNotFound(err) {
				return err
			}
			// First promotion into this environment: create the deployment.
			// Image and replicas come from the source release; the target
			// environment provides its own cluster and namespace.
			dep = &Deployment{
				ApplicationID: fromEnv.ApplicationID,
				ClusterID:     toEnv.ClusterID,
				Image:         srcRelease.Image,
				Replicas:      srcRelease.Replicas,
				EnvVars:       []byte("[]"),
				Status:        StatusPending,
				EnvironmentID: &toEnv.ID,
			}
			dep.OrgID = orgID
			dep.CreatedBy = &userID
			if err := s.deps.Create(ctx, dep); err != nil {
				return err
			}
		}

		// Create a new release in the target deployment, carrying the same
		// digest-pinned image from the source release (that is the invariant
		// of promotion: the exact same artifact, not just the same tag).
		latestRel, _ := s.rels.GetLatest(ctx, dep.ID)
		nextRevision := 1
		if latestRel != nil {
			nextRevision = latestRel.Revision + 1
		}

		config, _ := json.Marshal(map[string]any{
			"image":         srcRelease.Image,
			"replicas":      srcRelease.Replicas,
			"promotedFrom":  promo.FromEnvironmentID,
			"sourceRelease": promo.ReleaseID,
		})

		rel = &Release{
			OrgID:        orgID,
			DeploymentID: dep.ID,
			Revision:     nextRevision,
			Image:        srcRelease.Image,
			Replicas:     srcRelease.Replicas,
			ConfigHash:   hashConfig(config),
			Config:       config,
			Status:       ReleaseStatusPending,
		}
		rel.CreatedBy = &userID
		if err := s.rels.Create(ctx, rel); err != nil {
			return err
		}

		// Update the target deployment's image to match the promoted release.
		dep.Image = srcRelease.Image
		dep.Replicas = srcRelease.Replicas
		dep.Status = StatusPending
		if err := s.deps.Update(ctx, dep); err != nil {
			return err
		}

		// Gap 2 fix: atomic CAS on status='pending'.
		//
		// UpdateIfPending adds "AND status = 'pending'" to the WHERE clause.
		// If two concurrent ApprovePromotion calls race past the pre-auth status
		// check above, exactly one will find RowsAffected=1 (wins) and the other
		// will find RowsAffected=0 (CAS miss). On a miss we return an error, which
		// causes the entire WithTenant closure — including the dep Create, rel Create,
		// and dep Update executed earlier in this same closure — to be rolled back by
		// the enclosing DB transaction. The winning goroutine's writes remain intact.
		now := s.now()
		promo.Status = PromotionStatusApplied
		promo.ApprovedBy = &userID
		promo.ApprovedAt = &now
		promo.AppliedAt = &now
		applied, err := s.promos.UpdateIfPending(ctx, promo)
		if err != nil {
			return err
		}
		if !applied {
			// Another concurrent approval beat us: abort this transaction so
			// the dep/rel/dep-update writes made above are rolled back.
			return errPromotionNotPending
		}
		updatedPromo = promo
		return nil
	})

	return updatedPromo, dep, rel, err
}

// RejectPromotion rejects a pending promotion request.
// SECURITY: Requires ActionManageProject (same bar as ApprovePromotion).
func (s *Service) RejectPromotion(ctx context.Context, orgID, userID, promotionID string, req RejectPromotionRequest) (*PromotionRequest, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var promo *PromotionRequest
	var fromEnv *Environment
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		promo, err = s.promos.GetByID(ctx, promotionID)
		if err != nil {
			return errPromotionNotFound
		}
		if promo.Status != PromotionStatusPending {
			return errPromotionNotPending
		}
		fromEnv, err = s.envs.GetByID(ctx, promo.FromEnvironmentID)
		return err
	})
	if err != nil {
		return nil, err
	}

	if _, err := s.authorizeApplication(ctx, orgID, userID, fromEnv.ApplicationID, authz.ActionManageProject); err != nil {
		return nil, err
	}

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		reason := strings.TrimSpace(req.Reason)
		promo.Status = PromotionStatusRejected
		if reason != "" {
			promo.RejectionReason = &reason
		}
		return s.promos.Update(ctx, promo)
	})
	return promo, err
}
