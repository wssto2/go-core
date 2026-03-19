package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are the standard fields encoded in every JWT token.
// App-specific data is NOT stored in the token — it is loaded
// from the database after token validation by the app's resolver.
type Claims struct {
	jwt.RegisteredClaims

	UserID   int      `json:"user_id"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Roles    []string `json:"roles"`
	Language string   `json:"language"`
}

// TokenConfig holds the configuration required to sign and parse tokens.
type TokenConfig struct {
	SecretKey     string
	Issuer        string
	TokenDuration time.Duration
}

// ParseToken validates a JWT string and returns its claims.
// It returns ErrMissingToken, ErrInvalidToken, ErrExpiredToken, or ErrInvalidClaims
// depending on what failed — callers can use errors.Is() to handle each case.
func ParseToken(tokenString string, cfg TokenConfig) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrMissingToken
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(cfg.SecretKey), nil
	})

	if err != nil {
		// Distinguish expiry from other invalidity so callers can respond differently
		// (e.g. refresh vs. re-login)
		if isExpiredError(err) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidClaims
	}

	return claims, nil
}

// IssueToken creates and signs a new JWT token from the given claims.
func IssueToken(claims Claims, cfg TokenConfig) (string, error) {
	now := time.Now()

	claims.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    cfg.Issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(cfg.TokenDuration)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString([]byte(cfg.SecretKey))
	if err != nil {
		return "", fmt.Errorf("IssueToken: failed to sign: %w", err)
	}

	return signed, nil
}

func isExpiredError(err error) bool {
	return err != nil && err.Error() != "" &&
		(containsString(err.Error(), "token is expired") ||
			containsString(err.Error(), "expired"))
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
