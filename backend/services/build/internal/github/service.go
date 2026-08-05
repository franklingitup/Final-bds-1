package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// ServiceConfig holds GitHub service configuration.
type ServiceConfig struct {
	ClientID        string   // GitHub OAuth client ID
	ClientSecret    string   // GitHub OAuth client secret
	WebhookBaseURL  string   // Base URL for webhooks (e.g., https://api.example.com)
	EncryptionKey   string   // Base64-encoded AES-256 key for token encryption
	DefaultScopes   []string
	DefaultRegistry string   // Default container registry for webhook-triggered builds
}

// BuildTrigger creates builds from webhook events.
type BuildTrigger interface {
	TriggerBuildFromWebhook(ctx context.Context, orgID string, req WebhookBuildRequest) (string, error)
}

// WebhookBuildRequest contains parameters for a webhook-triggered build.
type WebhookBuildRequest struct {
	GitURL         string
	GitRef         string
	GitCommit      string
	TargetImage    string
	TargetRegistry string
	RepositoryName string
}

// Deps holds service dependencies.
type Deps struct {
	Connections       ConnectionStore
	Repositories      RepositoryStore
	Webhooks          WebhookStore
	WebhookDeliveries WebhookDeliveryStore
	OAuthStates       OAuthStateStore
	OrgMembers        authz.OrgMemberStore
	Outbox            events.Outbox
	Tenant            TenantRunner
	BuildTrigger      BuildTrigger // Optional: triggers builds from webhooks
	Logger            *slog.Logger
}

// TenantRunner runs a function within a tenant-scoped transaction.
type TenantRunner interface {
	WithTenant(ctx context.Context, orgID string, fn database.TxFunc) error
}

// Service implements GitHub integration business logic.
type Service struct {
	cfg          ServiceConfig
	client       *Client
	encryptor    *TokenEncryptor
	conns        ConnectionStore
	repos        RepositoryStore
	webhooks     WebhookStore
	deliveries   WebhookDeliveryStore
	states       OAuthStateStore
	orgMembers   authz.OrgMemberStore
	outbox       events.Outbox
	tenant       TenantRunner
	buildTrigger BuildTrigger
	authSvc      *authz.AuthorizationService
	log          *slog.Logger
}

// NewService creates a new GitHub service.
func NewService(cfg ServiceConfig, d Deps) (*Service, error) {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}

	var enc *TokenEncryptor
	if cfg.EncryptionKey != "" {
		var err error
		enc, err = NewTokenEncryptor(cfg.EncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("create token encryptor: %w", err)
		}
	}

	if cfg.DefaultScopes == nil {
		cfg.DefaultScopes = []string{"repo", "read:user", "admin:repo_hook"}
	}

	client := NewClient(ClientConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		WebhookURL:   cfg.WebhookBaseURL,
	}, "")

	return &Service{
		cfg:          cfg,
		client:       client,
		encryptor:    enc,
		conns:        d.Connections,
		repos:        d.Repositories,
		webhooks:     d.Webhooks,
		deliveries:   d.WebhookDeliveries,
		states:       d.OAuthStates,
		orgMembers:   d.OrgMembers,
		outbox:       d.Outbox,
		tenant:       d.Tenant,
		buildTrigger: d.BuildTrigger,
		authSvc:      authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil),
		log:          d.Logger,
	}, nil
}

// ----------------------------------------------------------------------------
// OAuth Flow
// ----------------------------------------------------------------------------

// GetOAuthURL generates an OAuth authorization URL.
func (s *Service) GetOAuthURL(ctx context.Context, orgID, userID string, redirectURL *string) (*OAuthURLResponse, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	state, err := GenerateOAuthState()
	if err != nil {
		return nil, fmt.Errorf("generate oauth state: %w", err)
	}

	oauthState := &GitHubOAuthState{
		OrgID:       orgID,
		UserID:      userID,
		State:       state,
		RedirectURL: redirectURL,
	}

	if err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.states.Create(ctx, oauthState)
	}); err != nil {
		return nil, err
	}

	redirectURI := ""
	if redirectURL != nil {
		redirectURI = *redirectURL
	}

	url := s.client.GetOAuthURL(state, redirectURI, s.cfg.DefaultScopes)

	return &OAuthURLResponse{
		URL:   url,
		State: state,
	}, nil
}

// HandleOAuthCallback handles the OAuth callback from GitHub.
func (s *Service) HandleOAuthCallback(ctx context.Context, code, state string) (*GitHubConnection, error) {
	// Look up the state to get orgID and userID
	var oauthState *GitHubOAuthState
	var err error

	// We need to find the state without tenant scope first
	oauthState, err = s.states.GetByState(ctx, state)
	if err != nil {
		return nil, apperrors.Validation("invalid or expired OAuth state")
	}

	orgID := oauthState.OrgID
	userID := oauthState.UserID

	// Exchange code for token
	token, err := s.client.ExchangeCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange oauth code: %w", err)
	}

	// Get user info
	userClient := s.client.WithToken(token.AccessToken)
	user, err := userClient.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("get github user: %w", err)
	}

	// Encrypt the token
	var encryptedToken []byte
	if s.encryptor != nil {
		encryptedToken, err = s.encryptor.Encrypt(token.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("encrypt token: %w", err)
		}
	} else {
		encryptedToken = []byte(token.AccessToken)
	}

	var encryptedRefresh []byte
	if token.RefreshToken != "" && s.encryptor != nil {
		encryptedRefresh, err = s.encryptor.Encrypt(token.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("encrypt refresh token: %w", err)
		}
	}

	scopes := strings.Split(token.Scope, ",")
	for i := range scopes {
		scopes[i] = strings.TrimSpace(scopes[i])
	}

	var tokenExpiry *time.Time
	if token.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
		tokenExpiry = &exp
	}

	tokenHash := HashToken(token.AccessToken)

	conn := &GitHubConnection{
		ConnectionType: ConnectionTypeOAuth,
		Name:           fmt.Sprintf("GitHub OAuth (%s)", user.Login),
		GitHubUserID:   &user.ID,
		GitHubUsername: &user.Login,
		GitHubAvatar:   &user.AvatarURL,
		AccessToken:    encryptedToken,
		RefreshToken:   encryptedRefresh,
		TokenExpiresAt: tokenExpiry,
		Scopes:         scopes,
		TokenHash:      &tokenHash,
		Status:         StatusActive,
	}
	conn.OrgID = orgID
	conn.CreatedBy = &userID

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Delete the used state
		_ = s.states.Delete(ctx, oauthState.ID)
		return s.conns.Create(ctx, conn)
	})
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// ----------------------------------------------------------------------------
// Connections (PAT)
// ----------------------------------------------------------------------------

// CreatePATConnection creates a new connection using a Personal Access Token.
func (s *Service) CreatePATConnection(ctx context.Context, orgID, userID string, req CreateConnectionRequest) (*GitHubConnection, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	if req.AccessToken == nil || *req.AccessToken == "" {
		return nil, apperrors.Validation("access token is required for PAT connection")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, apperrors.Validation("connection name is required")
	}

	// Validate the token by calling GitHub API
	testClient := s.client.WithToken(*req.AccessToken)
	user, err := testClient.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, apperrors.Validation("invalid GitHub token: " + err.Error())
	}

	// Encrypt the token
	var encryptedToken []byte
	if s.encryptor != nil {
		encryptedToken, err = s.encryptor.Encrypt(*req.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("encrypt token: %w", err)
		}
	} else {
		encryptedToken = []byte(*req.AccessToken)
	}

	tokenHash := HashToken(*req.AccessToken)
	now := time.Now()

	conn := &GitHubConnection{
		ConnectionType:  ConnectionTypePAT,
		Name:            name,
		GitHubUserID:    &user.ID,
		GitHubUsername:  &user.Login,
		GitHubAvatar:    &user.AvatarURL,
		AccessToken:     encryptedToken,
		TokenHash:       &tokenHash,
		LastValidatedAt: &now,
		Status:          StatusActive,
	}
	conn.OrgID = orgID
	conn.CreatedBy = &userID

	err = s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.conns.Create(ctx, conn)
	})
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// GetConnection returns a connection by ID.
func (s *Service) GetConnection(ctx context.Context, orgID, userID, connID string) (*GitHubConnection, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var conn *GitHubConnection
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		conn, err = s.conns.GetByID(ctx, connID)
		return err
	})
	return conn, err
}

// ListConnections returns all connections for an organization.
func (s *Service) ListConnections(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[GitHubConnection], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[GitHubConnection]{}, err
	}

	var out database.Page[GitHubConnection]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.conns.List(ctx, orgID, page)
		return err
	})
	return out, err
}

// DeleteConnection deletes a connection.
func (s *Service) DeleteConnection(ctx context.Context, orgID, userID, connID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.conns.Delete(ctx, connID)
	})
}

// ValidateConnection validates that a connection's token is still valid.
func (s *Service) ValidateConnection(ctx context.Context, orgID, userID, connID string) (*GitHubConnection, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var conn *GitHubConnection
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		conn, err = s.conns.GetByID(ctx, connID)
		if err != nil {
			return err
		}

		token, err := s.decryptToken(conn.AccessToken)
		if err != nil {
			return err
		}

		testClient := s.client.WithToken(token)
		_, err = testClient.GetAuthenticatedUser(ctx)
		if err != nil {
			errMsg := err.Error()
			conn.Status = StatusInvalid
			conn.ErrorMessage = &errMsg
			return s.conns.UpdateStatus(ctx, connID, StatusInvalid, &errMsg)
		}

		conn.Status = StatusActive
		conn.ErrorMessage = nil
		return s.conns.UpdateLastValidated(ctx, connID)
	})
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// ----------------------------------------------------------------------------
// Repositories
// ----------------------------------------------------------------------------

// ConnectRepository connects a GitHub repository.
func (s *Service) ConnectRepository(ctx context.Context, orgID, userID string, req ConnectRepositoryRequest) (*GitHubRepository, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var repo *GitHubRepository
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Get the connection
		conn, err := s.conns.GetByID(ctx, req.ConnectionID)
		if err != nil {
			return err
		}

		token, err := s.decryptToken(conn.AccessToken)
		if err != nil {
			return err
		}

		// Fetch repository from GitHub
		ghClient := s.client.WithToken(token)
		ghRepo, err := ghClient.GetRepository(ctx, req.Owner, req.Repo)
		if err != nil {
			return fmt.Errorf("fetch repository from GitHub: %w", err)
		}

		// Get languages
		languages, _ := ghClient.GetRepositoryLanguages(ctx, req.Owner, req.Repo)
		languagesJSON, _ := json.Marshal(languages)

		// Build permissions map
		perms := map[string]bool{
			"admin":    ghRepo.Permissions.Admin,
			"maintain": ghRepo.Permissions.Maintain,
			"push":     ghRepo.Permissions.Push,
			"triage":   ghRepo.Permissions.Triage,
			"pull":     ghRepo.Permissions.Pull,
		}
		permsJSON, _ := json.Marshal(perms)

		now := time.Now()
		repo = &GitHubRepository{
			ConnectionID:    conn.ID,
			GitHubRepoID:    ghRepo.ID,
			Owner:           ghRepo.Owner.Login,
			Name:            ghRepo.Name,
			FullName:        ghRepo.FullName,
			Description:     &ghRepo.Description,
			HTMLURL:         ghRepo.HTMLURL,
			CloneURL:        ghRepo.CloneURL,
			SSHURL:          &ghRepo.SSHURL,
			DefaultBranch:   ghRepo.DefaultBranch,
			IsPrivate:       ghRepo.Private,
			IsFork:          ghRepo.Fork,
			IsArchived:      ghRepo.Archived,
			StarsCount:      ghRepo.StargazersCount,
			ForksCount:      ghRepo.ForksCount,
			WatchersCount:   ghRepo.WatchersCount,
			OpenIssuesCount: ghRepo.OpenIssuesCount,
			Topics:          ghRepo.Topics,
			Language:        &ghRepo.Language,
			Languages:       languagesJSON,
			Permissions:     permsJSON,
			LastSyncedAt:    &now,
		}
		repo.OrgID = orgID
		repo.CreatedBy = &userID

		// Update connection last used
		_ = s.conns.UpdateLastUsed(ctx, conn.ID)

		return s.repos.Create(ctx, repo)
	})
	if err != nil {
		return nil, err
	}

	return repo, nil
}

// GetRepository returns a repository by ID.
func (s *Service) GetRepository(ctx context.Context, orgID, userID, repoID string) (*GitHubRepository, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var repo *GitHubRepository
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		repo, err = s.repos.GetByID(ctx, repoID)
		return err
	})
	return repo, err
}

// ListRepositories returns all repositories for an organization.
func (s *Service) ListRepositories(ctx context.Context, orgID, userID string, page database.PageRequest) (database.Page[GitHubRepository], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[GitHubRepository]{}, err
	}

	var out database.Page[GitHubRepository]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.repos.List(ctx, orgID, page)
		return err
	})
	return out, err
}

// DeleteRepository deletes a repository.
func (s *Service) DeleteRepository(ctx context.Context, orgID, userID, repoID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		return s.repos.Delete(ctx, repoID)
	})
}

// SyncRepository syncs repository metadata from GitHub.
func (s *Service) SyncRepository(ctx context.Context, orgID, userID, repoID string) (*GitHubRepository, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	var repo *GitHubRepository
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		repo, err = s.repos.GetByID(ctx, repoID)
		if err != nil {
			return err
		}

		conn, err := s.conns.GetByID(ctx, repo.ConnectionID)
		if err != nil {
			return err
		}

		token, err := s.decryptToken(conn.AccessToken)
		if err != nil {
			return err
		}

		ghClient := s.client.WithToken(token)
		ghRepo, err := ghClient.GetRepository(ctx, repo.Owner, repo.Name)
		if err != nil {
			syncErr := err.Error()
			_ = s.repos.UpdateSyncStatus(ctx, repoID, &syncErr)
			return err
		}

		// Update fields
		repo.Description = &ghRepo.Description
		repo.DefaultBranch = ghRepo.DefaultBranch
		repo.IsPrivate = ghRepo.Private
		repo.IsFork = ghRepo.Fork
		repo.IsArchived = ghRepo.Archived
		repo.StarsCount = ghRepo.StargazersCount
		repo.ForksCount = ghRepo.ForksCount
		repo.WatchersCount = ghRepo.WatchersCount
		repo.OpenIssuesCount = ghRepo.OpenIssuesCount
		repo.Topics = ghRepo.Topics
		repo.Language = &ghRepo.Language

		now := time.Now()
		repo.LastSyncedAt = &now
		repo.SyncError = nil

		return s.repos.Update(ctx, repo)
	})
	if err != nil {
		return nil, err
	}

	return repo, nil
}

// ----------------------------------------------------------------------------
// Branches & Commits
// ----------------------------------------------------------------------------

// ListBranches returns branches for a repository.
func (s *Service) ListBranches(ctx context.Context, orgID, userID, repoID string, page, perPage int) ([]BranchView, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 30
	}

	var branches []BranchView
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		repo, err := s.repos.GetByID(ctx, repoID)
		if err != nil {
			return err
		}

		conn, err := s.conns.GetByID(ctx, repo.ConnectionID)
		if err != nil {
			return err
		}

		token, err := s.decryptToken(conn.AccessToken)
		if err != nil {
			return err
		}

		ghClient := s.client.WithToken(token)
		ghBranches, err := ghClient.ListBranches(ctx, repo.Owner, repo.Name, page, perPage)
		if err != nil {
			return err
		}

		_ = s.conns.UpdateLastUsed(ctx, conn.ID)

		branches = make([]BranchView, len(ghBranches))
		for i, b := range ghBranches {
			branches[i] = BranchView{
				Name:      b.Name,
				SHA:       b.Commit.SHA,
				Protected: b.Protected,
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return branches, nil
}

// ListCommits returns commits for a repository branch.
func (s *Service) ListCommits(ctx context.Context, orgID, userID, repoID, branch string, page, perPage int) ([]CommitView, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 30
	}

	var commits []CommitView
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		repo, err := s.repos.GetByID(ctx, repoID)
		if err != nil {
			return err
		}

		conn, err := s.conns.GetByID(ctx, repo.ConnectionID)
		if err != nil {
			return err
		}

		token, err := s.decryptToken(conn.AccessToken)
		if err != nil {
			return err
		}

		ghClient := s.client.WithToken(token)
		ghCommits, err := ghClient.ListCommits(ctx, repo.Owner, repo.Name, branch, page, perPage)
		if err != nil {
			return err
		}

		_ = s.conns.UpdateLastUsed(ctx, conn.ID)

		commits = make([]CommitView, len(ghCommits))
		for i, c := range ghCommits {
			authorURL := ""
			if c.Author.HTMLURL != "" {
				authorURL = c.Author.HTMLURL
			}
			commits[i] = CommitView{
				SHA:       c.SHA,
				Message:   c.Commit.Message,
				Author:    c.Commit.Author.Name,
				AuthorURL: authorURL,
				Date:      c.Commit.Author.Date,
				URL:       c.HTMLURL,
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return commits, nil
}

// GetDefaultBranch returns the default branch for a repository.
func (s *Service) GetDefaultBranch(ctx context.Context, orgID, userID, repoID string) (string, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return "", err
	}

	var branch string
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		repo, err := s.repos.GetByID(ctx, repoID)
		if err != nil {
			return err
		}
		branch = repo.DefaultBranch
		return nil
	})
	return branch, err
}

// ----------------------------------------------------------------------------
// Webhooks
// ----------------------------------------------------------------------------

// RegisterWebhook registers a webhook on a GitHub repository.
func (s *Service) RegisterWebhook(ctx context.Context, orgID, userID, repoID string, req RegisterWebhookRequest) (*GitHubWebhook, error) {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return nil, err
	}

	events := req.Events
	if len(events) == 0 {
		events = []string{"push", "pull_request"}
	}

	var webhook *GitHubWebhook
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		repo, err := s.repos.GetByID(ctx, repoID)
		if err != nil {
			return err
		}

		conn, err := s.conns.GetByID(ctx, repo.ConnectionID)
		if err != nil {
			return err
		}

		token, err := s.decryptToken(conn.AccessToken)
		if err != nil {
			return err
		}

		// Generate webhook secret
		secret, err := GenerateWebhookSecret()
		if err != nil {
			return err
		}

		// Build webhook URL
		webhookURL := fmt.Sprintf("%s/v1/webhooks/github/%s", s.cfg.WebhookBaseURL, repoID)

		// Create webhook on GitHub
		ghClient := s.client.WithToken(token)
		hook, err := ghClient.CreateWebhook(ctx, repo.Owner, repo.Name, webhookURL, secret, events)
		if err != nil {
			return fmt.Errorf("create webhook on GitHub: %w", err)
		}

		// Encrypt the secret for storage
		var encryptedSecret []byte
		if s.encryptor != nil {
			encryptedSecret, err = s.encryptor.Encrypt(secret)
			if err != nil {
				return fmt.Errorf("encrypt webhook secret: %w", err)
			}
		} else {
			encryptedSecret = []byte(secret)
		}

		webhook = &GitHubWebhook{
			OrgID:        orgID,
			RepositoryID: repoID,
			GitHubHookID: hook.ID,
			Events:       events,
			WebhookURL:   webhookURL,
			Secret:       encryptedSecret,
			SecretHash:   HashToken(secret),
			Status:       WebhookStatusActive,
		}

		_ = s.conns.UpdateLastUsed(ctx, conn.ID)

		return s.webhooks.Create(ctx, webhook)
	})
	if err != nil {
		return nil, err
	}

	return webhook, nil
}

// GetWebhook returns a webhook by ID.
func (s *Service) GetWebhook(ctx context.Context, orgID, userID, webhookID string) (*GitHubWebhook, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	var webhook *GitHubWebhook
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		webhook, err = s.webhooks.GetByID(ctx, webhookID)
		return err
	})
	return webhook, err
}

// DeleteWebhook deletes a webhook.
func (s *Service) DeleteWebhook(ctx context.Context, orgID, userID, webhookID string) error {
	if _, err := s.authSvc.AuthorizeOrgMember(ctx, orgID, userID, authz.ActionDeploy); err != nil {
		return err
	}

	return s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		webhook, err := s.webhooks.GetByID(ctx, webhookID)
		if err != nil {
			return err
		}

		repo, err := s.repos.GetByID(ctx, webhook.RepositoryID)
		if err != nil {
			return err
		}

		conn, err := s.conns.GetByID(ctx, repo.ConnectionID)
		if err != nil {
			return err
		}

		token, err := s.decryptToken(conn.AccessToken)
		if err != nil {
			return err
		}

		// Delete webhook from GitHub
		ghClient := s.client.WithToken(token)
		if err := ghClient.DeleteWebhook(ctx, repo.Owner, repo.Name, webhook.GitHubHookID); err != nil {
			s.log.Warn("failed to delete webhook from GitHub", "hookId", webhook.GitHubHookID, "error", err)
		}

		return s.webhooks.Delete(ctx, webhookID)
	})
}

// HandleWebhookDelivery processes an incoming webhook delivery.
func (s *Service) HandleWebhookDelivery(ctx context.Context, repoID, deliveryID, eventType, signature string, payload []byte) (*GitHubWebhookDelivery, error) {
	// Get webhook without tenant scope (we need to find the org first)
	webhook, err := s.webhooks.GetByRepositoryID(ctx, repoID)
	if err != nil {
		return nil, apperrors.NotFound("webhook not found for repository")
	}

	// Decrypt the secret
	secret, err := s.decryptToken(webhook.Secret)
	if err != nil {
		return nil, fmt.Errorf("decrypt webhook secret: %w", err)
	}

	// Verify signature
	signatureValid := VerifyWebhookSignature(payload, secret, signature)

	// Parse payload to extract metadata
	var payloadData map[string]interface{}
	_ = json.Unmarshal(payload, &payloadData)

	var action, senderLogin, repositoryName, ref *string
	var senderID *int64
	var commitSHA, cloneURL string

	if a, ok := payloadData["action"].(string); ok {
		action = &a
	}
	if sender, ok := payloadData["sender"].(map[string]interface{}); ok {
		if login, ok := sender["login"].(string); ok {
			senderLogin = &login
		}
		if id, ok := sender["id"].(float64); ok {
			idInt := int64(id)
			senderID = &idInt
		}
	}
	if repo, ok := payloadData["repository"].(map[string]interface{}); ok {
		if name, ok := repo["full_name"].(string); ok {
			repositoryName = &name
		}
		if url, ok := repo["clone_url"].(string); ok {
			cloneURL = url
		}
	}
	if r, ok := payloadData["ref"].(string); ok {
		ref = &r
	}
	if after, ok := payloadData["after"].(string); ok {
		commitSHA = after
	}

	var headersJSON json.RawMessage
	headersJSON = []byte("{}")

	delivery := &GitHubWebhookDelivery{
		OrgID:            webhook.OrgID,
		WebhookID:        webhook.ID,
		GitHubDeliveryID: deliveryID,
		EventType:        eventType,
		Action:           action,
		Payload:          payload,
		Headers:          headersJSON,
		Signature:        &signature,
		SignatureValid:   signatureValid,
		Status:           DeliveryStatusReceived,
		SenderLogin:      senderLogin,
		SenderID:         senderID,
		RepositoryName:   repositoryName,
		Ref:              ref,
	}

	err = s.tenant.WithTenant(ctx, webhook.OrgID, func(ctx context.Context) error {
		if err := s.deliveries.Create(ctx, delivery); err != nil {
			return err
		}

		// Update webhook delivery stats
		var lastError *string
		if !signatureValid {
			errMsg := "signature verification failed"
			lastError = &errMsg
		}
		if updateErr := s.webhooks.UpdateDelivery(ctx, webhook.ID, lastError); updateErr != nil {
			s.log.Warn("failed to update webhook delivery stats", "error", updateErr)
		}

		// Trigger build for push events with valid signature
		if signatureValid && eventType == "push" && s.buildTrigger != nil {
			if err := s.triggerBuildFromPush(ctx, delivery, webhook.OrgID, ref, commitSHA, cloneURL, repositoryName); err != nil {
				s.log.Error("failed to trigger build from webhook", "error", err, "deliveryId", delivery.ID)
				errMsg := err.Error()
				delivery.Status = DeliveryStatusFailed
				delivery.ErrorMessage = &errMsg
			} else {
				delivery.Status = DeliveryStatusProcessed
			}
			now := time.Now()
			delivery.ProcessedAt = &now
			return s.deliveries.UpdateStatus(ctx, delivery.ID, delivery.Status, delivery.ErrorMessage)
		}

		// Mark non-push events or unsigned events as ignored
		if !signatureValid || eventType != "push" {
			delivery.Status = DeliveryStatusIgnored
			return s.deliveries.UpdateStatus(ctx, delivery.ID, delivery.Status, nil)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return delivery, nil
}

// triggerBuildFromPush creates a build from a push webhook event.
func (s *Service) triggerBuildFromPush(ctx context.Context, delivery *GitHubWebhookDelivery, orgID string, ref *string, commitSHA, cloneURL string, repositoryName *string) error {
	if s.buildTrigger == nil {
		return nil
	}

	if ref == nil || *ref == "" {
		return fmt.Errorf("missing ref in push event")
	}
	if commitSHA == "" {
		return fmt.Errorf("missing commit SHA in push event")
	}
	if cloneURL == "" {
		return fmt.Errorf("missing clone URL in push event")
	}

	// Extract branch name from ref (refs/heads/main -> main)
	branch := strings.TrimPrefix(*ref, "refs/heads/")

	// Skip tag pushes and deletions
	if strings.HasPrefix(*ref, "refs/tags/") {
		s.log.Info("skipping tag push", "ref", *ref)
		return nil
	}
	// Check for deletion (all zeros commit SHA)
	if commitSHA == "0000000000000000000000000000000000000000" {
		s.log.Info("skipping branch deletion", "ref", *ref)
		return nil
	}

	// Generate target image name: repo-name:branch-sha
	repoName := "app"
	if repositoryName != nil && *repositoryName != "" {
		// Use repo name part (owner/repo -> repo)
		parts := strings.Split(*repositoryName, "/")
		if len(parts) > 0 {
			repoName = strings.ToLower(parts[len(parts)-1])
		}
	}

	shortSHA := commitSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}
	targetImage := fmt.Sprintf("%s:%s-%s", repoName, branch, shortSHA)

	// Use default registry from config
	registry := s.cfg.DefaultRegistry
	if registry == "" {
		registry = "docker.io"
	}

	req := WebhookBuildRequest{
		GitURL:         cloneURL,
		GitRef:         branch,
		GitCommit:      commitSHA,
		TargetImage:    targetImage,
		TargetRegistry: registry,
		RepositoryName: repoName,
	}

	buildID, err := s.buildTrigger.TriggerBuildFromWebhook(ctx, orgID, req)
	if err != nil {
		return fmt.Errorf("trigger build: %w", err)
	}

	s.log.Info("triggered build from webhook",
		"buildId", buildID,
		"deliveryId", delivery.ID,
		"branch", branch,
		"commit", shortSHA,
	)

	return nil
}

// ListWebhookDeliveries returns deliveries for a webhook.
func (s *Service) ListWebhookDeliveries(ctx context.Context, orgID, userID, webhookID string, page database.PageRequest) (database.Page[GitHubWebhookDelivery], error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return database.Page[GitHubWebhookDelivery]{}, err
	}

	var out database.Page[GitHubWebhookDelivery]
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		var err error
		out, err = s.deliveries.List(ctx, webhookID, page)
		return err
	})
	return out, err
}

// ----------------------------------------------------------------------------
// Credential Provider
// ----------------------------------------------------------------------------

// GetGitToken returns a token for cloning a repository by its URL.
// This implements the builder.CredentialProvider interface.
func (s *Service) GetGitToken(ctx context.Context, orgID, repoURL string) (string, error) {
	// Parse owner/repo from the URL
	owner, repo := parseGitHubURL(repoURL)
	if owner == "" || repo == "" {
		return "", nil // Not a recognized GitHub URL
	}

	fullName := owner + "/" + repo

	var token string
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		// Find a GitHub repository matching this URL
		ghRepo, err := s.repos.GetByFullName(ctx, orgID, fullName)
		if err != nil {
			s.log.Debug("no GitHub repo found for URL", "fullName", fullName, "error", err)
			return nil // No matching repository registered, proceed without token
		}

		// Get the connection to retrieve the token
		conn, err := s.conns.GetByID(ctx, ghRepo.ConnectionID)
		if err != nil {
			return fmt.Errorf("get connection: %w", err)
		}

		// Decrypt and return the token
		token, err = s.decryptToken(conn.AccessToken)
		if err != nil {
			return fmt.Errorf("decrypt token: %w", err)
		}

		// Update last used timestamp
		_ = s.conns.UpdateLastUsed(ctx, conn.ID)
		return nil
	})
	if err != nil {
		return "", err
	}

	return token, nil
}

// parseGitHubURL extracts owner and repo from a GitHub URL.
// Supports HTTPS and SSH URLs.
func parseGitHubURL(url string) (owner, repo string) {
	// HTTPS: https://github.com/owner/repo.git or https://github.com/owner/repo
	if strings.Contains(url, "github.com/") {
		parts := strings.Split(url, "github.com/")
		if len(parts) == 2 {
			path := strings.TrimSuffix(parts[1], ".git")
			path = strings.TrimSuffix(path, "/")
			segments := strings.Split(path, "/")
			if len(segments) >= 2 {
				return segments[0], segments[1]
			}
		}
	}
	// SSH: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		path := strings.TrimPrefix(url, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		segments := strings.Split(path, "/")
		if len(segments) >= 2 {
			return segments[0], segments[1]
		}
	}
	return "", ""
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func (s *Service) decryptToken(encrypted []byte) (string, error) {
	if s.encryptor != nil {
		return s.encryptor.Decrypt(encrypted)
	}
	return string(encrypted), nil
}

// ListUserGitHubRepositories lists repositories from GitHub for a connection.
func (s *Service) ListUserGitHubRepositories(ctx context.Context, orgID, userID, connID string, page, perPage int) ([]Repository, error) {
	if _, err := s.authSvc.AuthorizeOrgRead(ctx, orgID, userID); err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 30
	}

	var repos []Repository
	err := s.tenant.WithTenant(ctx, orgID, func(ctx context.Context) error {
		conn, err := s.conns.GetByID(ctx, connID)
		if err != nil {
			return err
		}

		token, err := s.decryptToken(conn.AccessToken)
		if err != nil {
			return err
		}

		ghClient := s.client.WithToken(token)
		repos, err = ghClient.ListUserRepositories(ctx, page, perPage)
		if err != nil {
			return err
		}

		_ = s.conns.UpdateLastUsed(ctx, conn.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return repos, nil
}
