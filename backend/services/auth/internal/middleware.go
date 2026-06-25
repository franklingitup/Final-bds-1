package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/authz"
)

// Fiber Locals keys for the authenticated principal.
const (
	localUserID    = "auth_user_id"
	localUserEmail = "auth_user_email"
)

// VerifyAccessToken validates an access token and returns its claims. Exposed so
// the gateway and other services can reuse the auth service's verification.
func (s *Service) VerifyAccessToken(token string) (*AccessClaims, error) {
	return s.jwt.Verify(token)
}

// RequireAuth is middleware that authenticates the request via a Bearer access
// token, storing the user identity in the request locals and context.
func (h *Handler) RequireAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := bearerToken(c)
		if token == "" {
			return errInvalidToken
		}
		claims, err := h.svc.VerifyAccessToken(token)
		if err != nil {
			return errInvalidToken
		}
		c.Locals(localUserID, claims.Subject)
		c.Locals(localUserEmail, claims.Email)
		c.SetUserContext(authz.WithPrincipal(c.UserContext(), authz.Principal{
			UserID: claims.Subject,
		}))
		return c.Next()
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(c *fiber.Ctx) string {
	header := c.Get(fiber.HeaderAuthorization)
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// currentUserID returns the authenticated user's ID from request locals.
func currentUserID(c *fiber.Ctx) string {
	id, _ := c.Locals(localUserID).(string)
	return id
}
