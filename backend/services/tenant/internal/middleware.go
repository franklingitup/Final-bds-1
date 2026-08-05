package tenant

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/authz"
)

// Fiber Locals keys for the authenticated caller.
const (
	localUserID = "tenant_user_id"
	localEmail  = "tenant_user_email"
)

// RequireAuth authenticates the request via a Bearer access token issued by the
// auth service, storing the caller identity in request locals and context.
func (h *Handler) RequireAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := bearerToken(c)
		if token == "" {
			return errInvalidToken
		}

		id, err := h.verifier.Verify(token)
		if err != nil {
			return errInvalidToken
		}

		c.Locals(localUserID, id.UserID)
		c.Locals(localEmail, id.Email)
		c.SetUserContext(authz.WithPrincipal(c.UserContext(), authz.Principal{UserID: id.UserID}))
		return c.Next()
	}
}

func bearerToken(c *fiber.Ctx) string {
	header := c.Get(fiber.HeaderAuthorization)
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func callerIdentity(c *fiber.Ctx) Identity {
	uid, _ := c.Locals(localUserID).(string)
	email, _ := c.Locals(localEmail).(string)
	return Identity{UserID: uid, Email: email}
}
