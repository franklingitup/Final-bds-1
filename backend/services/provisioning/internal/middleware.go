package provisioning

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// TokenVerifier verifies JWT tokens.
type TokenVerifier interface {
	Verify(tokenString string) (*Identity, error)
}

// RequireAuth returns middleware that requires a valid JWT.
func RequireAuth(verifier TokenVerifier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if auth == "" {
			return apperrors.Unauthenticated("missing authorization header")
		}

		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			return apperrors.Unauthenticated("invalid authorization header format")
		}

		id, err := verifier.Verify(parts[1])
		if err != nil {
			return apperrors.Unauthenticated("invalid or expired token")
		}

		c.Locals("identity", id)
		return c.Next()
	}
}
