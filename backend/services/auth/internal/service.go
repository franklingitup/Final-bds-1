package auth

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/config"
	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/events"
)

// Security and lifecycle tunables.
const (
	maxFailedLogins  = 5
	lockoutDuration  = 15 * time.Minute
	emailVerifyTTL   = 24 * time.Hour
	passwordResetTTL = time.Hour
	minPasswordLen   = 8
	mfaIssuerLabel   = "BDS Platform"
)

// RequestMeta carries non-authoritative request context recorded on sessions.
type RequestMeta struct {
	UserAgent string
	IP        string
}

// Deps are the AuthService dependencies. Stores are interfaces so the service
// is unit-testable with in-memory fakes.
type Deps struct {
	Users           UserStore
	Sessions        SessionStore
	OneTimeTokens   OneTimeTokenStore
	ServiceAccounts ServiceAccountStore
	APITokens       APITokenStore
	OrgMembers      authz.OrgMemberStore // For org membership authorization
	Tx              Transactor
	Tenant          TenantRunner
	JWT             *JWTIssuer
	// Outbox persists domain events in the same transaction as the state change
	// that produced them; a relay publishes them to the broker.
	Outbox events.Outbox
	// Notifier delivers one-time tokens over a secure channel (optional).
	Notifier Notifier
	Auth     config.AuthConfig
	Logger   *slog.Logger
	Now      func() time.Time
}

// Service implements the auth domain logic.
type Service struct {
	users           UserStore
	sessions        SessionStore
	otps            OneTimeTokenStore
	serviceAccounts ServiceAccountStore
	apiTokens       APITokenStore
	orgMembers      authz.OrgMemberStore
	tx              Transactor
	tenant          TenantRunner
	jwt             *JWTIssuer
	outbox          events.Outbox
	authSvc         *authz.AuthorizationService
	notifier        Notifier
	cfg             config.AuthConfig
	log             *slog.Logger
	now             func() time.Time
}

// NewService wires an AuthService from its dependencies.
func NewService(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}

	// Create authorization service if org members store is provided
	var authSvc *authz.AuthorizationService
	if d.OrgMembers != nil {
		authSvc = authz.NewAuthorizationService(d.Tenant, d.OrgMembers, nil)
	}

	return &Service{
		users:           d.Users,
		sessions:        d.Sessions,
		otps:            d.OneTimeTokens,
		serviceAccounts: d.ServiceAccounts,
		apiTokens:       d.APITokens,
		orgMembers:      d.OrgMembers,
		tx:              d.Tx,
		tenant:          d.Tenant,
		jwt:             d.JWT,
		outbox:          d.Outbox,
		authSvc:         authSvc,
		notifier:        d.Notifier,
		cfg:             d.Auth,
		log:             d.Logger,
		now:             d.Now,
	}
}

// ----------------------------------------------------------------------------
// Signup & login
// ----------------------------------------------------------------------------

// Signup creates a user, issues an initial token pair, and emits a signup event
// carrying a one-time email-verification token for the notification service to
// deliver. The verification token is never returned via the API.
func (s *Service) Signup(ctx context.Context, req SignupRequest, meta RequestMeta) (*TokenPair, error) {
	email := normalizeEmail(req.Email)
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if err := validatePassword(req.Password); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, apperrors.Validation("name is required")
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	user := &User{
		Email:        email,
		Name:         strings.TrimSpace(req.Name),
		PasswordHash: hash,
		Status:       UserStatusActive,
	}

	var (
		pair        *TokenPair
		verifyToken string
		deliveryRef string
	)
	err = s.tx.Tx(ctx, func(ctx context.Context) error {
		if err := s.users.Create(ctx, user); err != nil {
			if apperrors.From(err).Code == apperrors.CodeConflict {
				return errEmailTaken
			}
			return err
		}
		verifyToken, deliveryRef, err = s.createOneTimeToken(ctx, user.ID, PurposeEmailVerify, emailVerifyTTL)
		if err != nil {
			return err
		}
		if pair, err = s.issueTokenPair(ctx, user, true, meta); err != nil {
			return err
		}
		if err := s.enqueue(ctx, EventUserCreated, systemOrg, userCreatedPayload{
			UserID: user.ID, Email: user.Email, Name: user.Name,
		}, events.WithActor(events.Actor{Type: "user", ID: user.ID}),
			events.WithResource(events.Resource{Type: "user", ID: user.ID})); err != nil {
			return err
		}
		return s.enqueue(ctx, EventEmailVerificationRequested, systemOrg, emailVerificationRequestedPayload{
			UserID: user.ID, Email: user.Email, Purpose: PurposeEmailVerification, DeliveryRef: deliveryRef,
		}, events.WithResource(events.Resource{Type: "user", ID: user.ID}))
	})
	if err != nil {
		return nil, err
	}

	s.notify(ctx, TokenDelivery{
		Purpose: PurposeEmailVerification, UserID: user.ID, Email: user.Email,
		Token: verifyToken, DeliveryRef: deliveryRef,
	})
	return pair, nil
}

// Login authenticates a user and returns a token pair, enforcing account
// lockout and MFA.
func (s *Service) Login(ctx context.Context, req LoginRequest, meta RequestMeta) (*TokenPair, error) {
	email := normalizeEmail(req.Email)
	if email == "" || req.Password == "" {
		return nil, errInvalidCredentials
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, errInvalidCredentials
		}
		return nil, err
	}

	now := s.now()
	if user.IsLocked(now) {
		return nil, errAccountLocked
	}
	if user.Status == UserStatusDisabled {
		return nil, errInvalidCredentials
	}

	ok, err := VerifyPassword(req.Password, user.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.registerFailedLogin(ctx, user)
		return nil, errInvalidCredentials
	}

	// MFA gate.
	if user.MFAEnabled {
		if strings.TrimSpace(req.MFACode) == "" {
			return nil, errMFARequired
		}
		if user.MFASecret == nil || !verifyTOTP(*user.MFASecret, req.MFACode, now) {
			s.registerFailedLogin(ctx, user)
			return nil, errMFAInvalid
		}
	}

	var pair *TokenPair
	err = s.tx.Tx(ctx, func(ctx context.Context) error {
		// Reset lockout counters on success.
		if user.FailedLoginAttempts != 0 || user.LockedUntil != nil {
			user.FailedLoginAttempts = 0
			user.LockedUntil = nil
			if err := s.users.Update(ctx, user); err != nil {
				return err
			}
		}
		if pair, err = s.issueTokenPair(ctx, user, true, meta); err != nil {
			return err
		}
		return s.enqueue(ctx, EventLoginSucceeded, systemOrg, loginSucceededPayload{
			UserID: user.ID, Email: user.Email,
		}, events.WithActor(events.Actor{Type: "user", ID: user.ID}))
	})
	if err != nil {
		return nil, err
	}
	return pair, nil
}

// registerFailedLogin increments the failure counter and locks the account when
// the threshold is reached, recording the failed-login event in the same
// transaction. Failures here are best-effort and never mask the original auth
// error.
func (s *Service) registerFailedLogin(ctx context.Context, user *User) {
	user.FailedLoginAttempts++
	if user.FailedLoginAttempts >= maxFailedLogins {
		until := s.now().Add(lockoutDuration)
		user.LockedUntil = &until
	}
	err := s.tx.Tx(ctx, func(ctx context.Context) error {
		if err := s.users.Update(ctx, user); err != nil {
			return err
		}
		return s.enqueue(ctx, EventLoginFailed, systemOrg, loginFailedPayload{
			UserID: user.ID, Email: user.Email, Attempts: user.FailedLoginAttempts,
		}, events.WithActor(events.Actor{Type: "user", ID: user.ID}))
	})
	if err != nil {
		s.log.Warn("failed to record login failure", "userId", user.ID, "error", err)
	}
}

// ----------------------------------------------------------------------------
// Token lifecycle
// ----------------------------------------------------------------------------

// Refresh rotates a refresh token: the presented token is revoked and replaced
// with a new one, and a fresh access token is issued.
func (s *Service) Refresh(ctx context.Context, req RefreshRequest, meta RequestMeta) (*TokenPair, error) {
	if strings.TrimSpace(req.RefreshToken) == "" {
		return nil, errInvalidToken
	}
	session, err := s.sessions.GetByHash(ctx, hashToken(req.RefreshToken))
	if err != nil {
		if database.IsNotFound(err) {
			return nil, errInvalidToken
		}
		return nil, err
	}

	now := s.now()
	if session.RevokedAt != nil {
		return nil, errTokenRevoked
	}
	if !session.Active(now) {
		return nil, errInvalidToken
	}

	user, err := s.users.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	if user.IsLocked(now) || user.Status == UserStatusDisabled {
		return nil, errAccountLocked
	}

	var pair *TokenPair
	err = s.tx.Tx(ctx, func(ctx context.Context) error {
		pair, err = s.issueTokenPair(ctx, user, false, meta)
		if err != nil {
			return err
		}
		// Rotate: revoke the presented session, pointing at its replacement.
		var newSessionID string
		newSession, lookupErr := s.sessions.GetByHash(ctx, hashToken(pair.RefreshToken))
		if lookupErr == nil && newSession != nil {
			newSessionID = newSession.ID
			if err := s.sessions.Revoke(ctx, session.ID, &newSession.ID); err != nil {
				return err
			}
		} else if err := s.sessions.Revoke(ctx, session.ID, nil); err != nil {
			return err
		}
		return s.enqueue(ctx, EventTokenRotated, systemOrg, tokenRotatedPayload{
			UserID: user.ID, SessionID: newSessionID, ReplacedSessionID: session.ID,
		}, events.WithActor(events.Actor{Type: "user", ID: user.ID}))
	})
	if err != nil {
		return nil, err
	}
	return pair, nil
}

// Logout revokes the presented refresh token. It is idempotent: an unknown or
// already-revoked token still succeeds.
func (s *Service) Logout(ctx context.Context, req LogoutRequest) error {
	if strings.TrimSpace(req.RefreshToken) == "" {
		return nil
	}
	session, err := s.sessions.GetByHash(ctx, hashToken(req.RefreshToken))
	if err != nil {
		if database.IsNotFound(err) {
			return nil
		}
		return err
	}
	return s.tx.Tx(ctx, func(ctx context.Context) error {
		if err := s.sessions.Revoke(ctx, session.ID, nil); err != nil {
			return err
		}
		return s.enqueue(ctx, EventTokenRevoked, systemOrg, tokenRevokedPayload{
			UserID: session.UserID, SessionID: session.ID,
		}, events.WithActor(events.Actor{Type: "user", ID: session.UserID}))
	})
}

// issueTokenPair mints an access token and a new refresh-token session. It must
// run inside a transaction so the session row commits with the caller's work.
func (s *Service) issueTokenPair(ctx context.Context, u *User, includeUser bool, meta RequestMeta) (*TokenPair, error) {
	access, _, expiresIn, err := s.jwt.Issue(u.ID, u.Email)
	if err != nil {
		return nil, err
	}
	refreshPlain, err := generateSecret(32)
	if err != nil {
		return nil, err
	}
	session := &RefreshToken{
		UserID:    u.ID,
		TokenHash: hashToken(refreshPlain),
		ExpiresAt: s.now().Add(s.cfg.RefreshTTL),
		UserAgent: optionalString(meta.UserAgent),
		IP:        optionalString(meta.IP),
	}
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, err
	}
	pair := &TokenPair{AccessToken: access, RefreshToken: refreshPlain, ExpiresIn: expiresIn}
	if includeUser {
		pair.User = u.Profile()
	}
	return pair, nil
}

// ----------------------------------------------------------------------------
// Email verification & password reset
// ----------------------------------------------------------------------------

// VerifyEmail consumes an email-verification token and marks the user verified.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	return s.consumeOneTimeToken(ctx, token, PurposeEmailVerify, func(ctx context.Context, user *User) error {
		if user.EmailVerified {
			return nil
		}
		user.EmailVerified = true
		if err := s.users.Update(ctx, user); err != nil {
			return err
		}
		return s.enqueue(ctx, EventEmailVerified, systemOrg, emailVerifiedPayload{
			UserID: user.ID, Email: user.Email,
		}, events.WithActor(events.Actor{Type: "user", ID: user.ID}),
			events.WithResource(events.Resource{Type: "user", ID: user.ID}))
	})
}

// ResendVerification issues a fresh verification token. To avoid account
// enumeration it always reports success, even for unknown emails.
func (s *Service) ResendVerification(ctx context.Context, email string) error {
	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if database.IsNotFound(err) {
			return nil
		}
		return err
	}
	if user.EmailVerified {
		return nil
	}
	var (
		token       string
		deliveryRef string
	)
	err = s.tx.Tx(ctx, func(ctx context.Context) error {
		if err := s.otps.InvalidateForUser(ctx, user.ID, PurposeEmailVerify); err != nil {
			return err
		}
		token, deliveryRef, err = s.createOneTimeToken(ctx, user.ID, PurposeEmailVerify, emailVerifyTTL)
		if err != nil {
			return err
		}
		return s.enqueue(ctx, EventEmailVerificationRequested, systemOrg, emailVerificationRequestedPayload{
			UserID: user.ID, Email: user.Email, Purpose: PurposeEmailVerification, DeliveryRef: deliveryRef,
		}, events.WithResource(events.Resource{Type: "user", ID: user.ID}))
	})
	if err != nil {
		return err
	}
	s.notify(ctx, TokenDelivery{
		Purpose: PurposeEmailVerification, UserID: user.ID, Email: user.Email,
		Token: token, DeliveryRef: deliveryRef,
	})
	return nil
}

// RequestPasswordReset issues a password-reset token. Always reports success to
// avoid account enumeration.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if database.IsNotFound(err) {
			return nil
		}
		return err
	}
	var (
		token       string
		deliveryRef string
	)
	err = s.tx.Tx(ctx, func(ctx context.Context) error {
		if err := s.otps.InvalidateForUser(ctx, user.ID, PurposePasswordReset); err != nil {
			return err
		}
		token, deliveryRef, err = s.createOneTimeToken(ctx, user.ID, PurposePasswordReset, passwordResetTTL)
		if err != nil {
			return err
		}
		return s.enqueue(ctx, EventPasswordReset, systemOrg, passwordResetPayload{
			UserID: user.ID, Phase: "requested", Email: user.Email,
			Purpose: PurposePasswordResetMsg, DeliveryRef: deliveryRef,
		}, events.WithResource(events.Resource{Type: "user", ID: user.ID}))
	})
	if err != nil {
		return err
	}
	s.notify(ctx, TokenDelivery{
		Purpose: PurposePasswordResetMsg, UserID: user.ID, Email: user.Email,
		Token: token, DeliveryRef: deliveryRef,
	})
	return nil
}

// ConfirmPasswordReset sets a new password from a valid reset token and revokes
// all existing sessions.
func (s *Service) ConfirmPasswordReset(ctx context.Context, token, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.consumeOneTimeToken(ctx, token, PurposePasswordReset, func(ctx context.Context, user *User) error {
		user.PasswordHash = hash
		user.FailedLoginAttempts = 0
		user.LockedUntil = nil
		if err := s.users.Update(ctx, user); err != nil {
			return err
		}
		if err := s.sessions.RevokeAllForUser(ctx, user.ID); err != nil {
			return err
		}
		return s.enqueue(ctx, EventPasswordReset, systemOrg, passwordResetPayload{
			UserID: user.ID, Phase: "completed",
		}, events.WithActor(events.Actor{Type: "user", ID: user.ID}),
			events.WithResource(events.Resource{Type: "user", ID: user.ID}))
	})
}

// createOneTimeToken generates a one-time token, stores its hash, and returns
// the plaintext (for secure out-of-band delivery) plus the row id (used as the
// stable delivery reference published on the domain event).
func (s *Service) createOneTimeToken(ctx context.Context, userID, purpose string, ttl time.Duration) (plaintext, id string, err error) {
	plaintext, err = generateSecret(32)
	if err != nil {
		return "", "", err
	}
	ott := &OneTimeToken{
		UserID:    userID,
		Purpose:   purpose,
		TokenHash: hashToken(plaintext),
		ExpiresAt: s.now().Add(ttl),
	}
	if err := s.otps.Create(ctx, ott); err != nil {
		return "", "", err
	}
	return plaintext, ott.ID, nil
}

// consumeOneTimeToken validates a one-time token of the given purpose, marks it
// used, and runs apply with the owning user, all in one transaction.
func (s *Service) consumeOneTimeToken(ctx context.Context, token, purpose string, apply func(context.Context, *User) error) error {
	if strings.TrimSpace(token) == "" {
		return errInvalidToken
	}
	return s.tx.Tx(ctx, func(ctx context.Context) error {
		ott, err := s.otps.GetByHash(ctx, hashToken(token))
		if err != nil {
			if database.IsNotFound(err) {
				return errInvalidToken
			}
			return err
		}
		if ott.Purpose != purpose || !ott.Usable(s.now()) {
			return errInvalidToken
		}
		user, err := s.users.GetByID(ctx, ott.UserID)
		if err != nil {
			return err
		}
		if err := s.otps.MarkUsed(ctx, ott.ID); err != nil {
			return err
		}
		return apply(ctx, user)
	})
}

// ----------------------------------------------------------------------------
// Profile & MFA
// ----------------------------------------------------------------------------

// Me returns the authenticated user's profile.
func (s *Service) Me(ctx context.Context, userID string) (*UserProfile, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return user.Profile(), nil
}

// SetupMFA generates a TOTP secret for the user (not yet enabled) and returns
// the secret plus an otpauth URL for provisioning into an authenticator app.
func (s *Service) SetupMFA(ctx context.Context, userID string) (*MFASetupResult, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		return nil, err
	}
	user.MFASecret = &secret
	user.MFAEnabled = false
	err = s.tx.Tx(ctx, func(ctx context.Context) error {
		if err := s.users.Update(ctx, user); err != nil {
			return err
		}
		return s.enqueue(ctx, EventMFASetupStarted, systemOrg, mfaPayload{UserID: user.ID},
			events.WithActor(events.Actor{Type: "user", ID: user.ID}),
			events.WithResource(events.Resource{Type: "user", ID: user.ID}))
	})
	if err != nil {
		return nil, err
	}
	return &MFASetupResult{
		Secret:     secret,
		OTPAuthURL: otpauthURL(mfaIssuerLabel, user.Email, secret),
	}, nil
}

// EnableMFA verifies a TOTP code against the pending secret and enables MFA.
func (s *Service) EnableMFA(ctx context.Context, userID, code string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.MFASecret == nil {
		return apperrors.Validation("MFA setup has not been started")
	}
	if !verifyTOTP(*user.MFASecret, code, s.now()) {
		return errMFAInvalid
	}
	user.MFAEnabled = true
	return s.tx.Tx(ctx, func(ctx context.Context) error {
		if err := s.users.Update(ctx, user); err != nil {
			return err
		}
		return s.enqueue(ctx, EventMFAEnabled, systemOrg, mfaPayload{UserID: user.ID},
			events.WithActor(events.Actor{Type: "user", ID: user.ID}),
			events.WithResource(events.Resource{Type: "user", ID: user.ID}))
	})
}

// DisableMFA verifies a TOTP code and disables MFA, clearing the secret.
func (s *Service) DisableMFA(ctx context.Context, userID, code string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if !user.MFAEnabled || user.MFASecret == nil {
		return apperrors.Validation("MFA is not enabled")
	}
	if !verifyTOTP(*user.MFASecret, code, s.now()) {
		return errMFAInvalid
	}
	user.MFAEnabled = false
	user.MFASecret = nil
	return s.tx.Tx(ctx, func(ctx context.Context) error {
		if err := s.users.Update(ctx, user); err != nil {
			return err
		}
		return s.enqueue(ctx, EventMFADisabled, systemOrg, mfaPayload{UserID: user.ID},
			events.WithActor(events.Actor{Type: "user", ID: user.ID}),
			events.WithResource(events.Resource{Type: "user", ID: user.ID}))
	})
}

// ----------------------------------------------------------------------------
// Validation helpers
// ----------------------------------------------------------------------------

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateEmail(email string) error {
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || !strings.Contains(email[at+1:], ".") {
		return apperrors.Validation("a valid email address is required")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLen {
		return apperrors.Validation("password must be at least 8 characters")
	}
	return nil
}

func optionalString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
