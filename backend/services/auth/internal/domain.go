package auth

import (
	"time"

	"github.com/bdsplatform/platform/backend/libs/database"
)

// User account statuses.
const (
	UserStatusActive   = "active"
	UserStatusLocked   = "locked"
	UserStatusDisabled = "disabled"
)

// One-time token purposes.
const (
	PurposeEmailVerify   = "email_verify"
	PurposePasswordReset = "password_reset"
)

// User is a global identity. It is not tenant-scoped: a user may belong to many
// organizations (membership is owned by the tenant service).
type User struct {
	database.Model
	Email               string     `db:"email"`
	Name                string     `db:"name"`
	PasswordHash        string     `db:"password_hash"`
	Status              string     `db:"status"`
	EmailVerified       bool       `db:"email_verified"`
	MFAEnabled          bool       `db:"mfa_enabled"`
	MFASecret           *string    `db:"mfa_secret"`
	FailedLoginAttempts int        `db:"failed_login_attempts"`
	LockedUntil         *time.Time `db:"locked_until"`
}

// IsLocked reports whether the account is currently locked out.
func (u *User) IsLocked(now time.Time) bool {
	if u.Status == UserStatusLocked {
		return true
	}
	return u.LockedUntil != nil && u.LockedUntil.After(now)
}

// RefreshToken is a rotating, revocable session. Only the hash is persisted.
type RefreshToken struct {
	database.Model
	UserID     string     `db:"user_id"`
	TokenHash  string     `db:"token_hash"`
	UserAgent  *string    `db:"user_agent"`
	IP         *string    `db:"ip"`
	ExpiresAt  time.Time  `db:"expires_at"`
	RevokedAt  *time.Time `db:"revoked_at"`
	ReplacedBy *string    `db:"replaced_by"`
}

// Active reports whether the refresh token can still be used.
func (t *RefreshToken) Active(now time.Time) bool {
	return t.RevokedAt == nil && t.ExpiresAt.After(now)
}

// OneTimeToken backs email verification and password reset. Only the hash is
// persisted; the plaintext is delivered out of band (email).
type OneTimeToken struct {
	ID        string     `db:"id"`
	UserID    string     `db:"user_id"`
	Purpose   string     `db:"purpose"`
	TokenHash string     `db:"token_hash"`
	ExpiresAt time.Time  `db:"expires_at"`
	UsedAt    *time.Time `db:"used_at"`
	CreatedAt time.Time  `db:"created_at"`
}

// Usable reports whether the token is unused and unexpired.
func (t *OneTimeToken) Usable(now time.Time) bool {
	return t.UsedAt == nil && t.ExpiresAt.After(now)
}

// ServiceAccount is a machine identity scoped to an organization.
type ServiceAccount struct {
	database.TenantModel
	Name        string  `db:"name"`
	Description *string `db:"description"`
	Status      string  `db:"status"`
	CreatedBy   *string `db:"created_by"`
}

// APIToken is a scoped, revocable credential owned by a service account. Only
// the hash is persisted; the plaintext is shown once at creation.
type APIToken struct {
	database.TenantModel
	ServiceAccountID string     `db:"service_account_id"`
	Name             string     `db:"name"`
	Prefix           string     `db:"prefix"`
	TokenHash        string     `db:"token_hash"`
	Scopes           []string   `db:"scopes"`
	ExpiresAt        *time.Time `db:"expires_at"`
	LastUsedAt       *time.Time `db:"last_used_at"`
	RevokedAt        *time.Time `db:"revoked_at"`
}

// ----------------------------------------------------------------------------
// Request / response DTOs (see docs/04-api-spec.md section 1).
// ----------------------------------------------------------------------------

type SignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	MFACode  string `json:"mfaCode,omitempty"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type VerifyEmailRequest struct {
	Token string `json:"token"`
}

type ResendVerificationRequest struct {
	Email string `json:"email"`
}

type PasswordResetRequest struct {
	Email string `json:"email"`
}

type PasswordResetConfirmRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

type MFAEnableRequest struct {
	Code string `json:"code"`
}

type MFADisableRequest struct {
	Code string `json:"code"`
}

type CreateServiceAccountRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type CreateAPITokenRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes,omitempty"`
	ExpiresInDays int      `json:"expiresInDays,omitempty"`
}

// UserProfile is the public projection of a user.
type UserProfile struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	EmailVerified bool   `json:"emailVerified"`
	MFAEnabled    bool   `json:"mfaEnabled"`
}

// TokenPair is the access + refresh token result returned by login/refresh.
type TokenPair struct {
	AccessToken  string       `json:"accessToken"`
	RefreshToken string       `json:"refreshToken"`
	ExpiresIn    int          `json:"expiresIn"` // access token TTL in seconds
	User         *UserProfile `json:"user,omitempty"`
}

// MFASetupResult is returned when a user begins MFA enrollment.
type MFASetupResult struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauthUrl"`
}

// Profile builds the public projection of a user.
func (u *User) Profile() *UserProfile {
	return &UserProfile{
		ID:            u.ID,
		Email:         u.Email,
		Name:          u.Name,
		EmailVerified: u.EmailVerified,
		MFAEnabled:    u.MFAEnabled,
	}
}
