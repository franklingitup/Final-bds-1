package notification

import (
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JWTVerifier verifies JWT tokens.
type JWTVerifier struct {
	signingKey []byte
}

// NewJWTVerifier creates a new JWT verifier.
func NewJWTVerifier(signingKey string) *JWTVerifier {
	return &JWTVerifier{signingKey: []byte(signingKey)}
}

// Verify parses and verifies a JWT token.
func (v *JWTVerifier) Verify(tokenString string) (*Identity, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return v.signingKey, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}

	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)

	// Handle sub formats: "user:<uuid>" or just "<uuid>"
	userID := sub
	if strings.HasPrefix(sub, "user:") {
		userID = strings.TrimPrefix(sub, "user:")
	}

	return &Identity{
		UserID: userID,
		Email:  email,
	}, nil
}
