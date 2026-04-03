package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"
)

// GenerateRefreshToken produces a URL-safe random token string.
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// RotateRefreshToken atomically finds the token by oldRefresh, validates it,
// generates a new refresh token, updates the store, and returns the new token.
// The find and rotate happen in a single transaction to prevent replay attacks
// from concurrent requests with the same token.
// If the provided refresh token is invalid, expired, or revoked, ErrUnauthorized
// is returned.
func RotateRefreshToken(ctx context.Context, store TokenStore, oldRefresh string, newTTL time.Duration) (string, *Token, error) {
	newRefresh, err := GenerateRefreshToken()
	if err != nil {
		return "", nil, err
	}

	newExpiry := time.Now().Add(newTTL)
	tok, err := store.FindAndRotateRefreshToken(ctx, oldRefresh, newRefresh, newExpiry)
	if err != nil {
		return "", nil, ErrUnauthorized
	}

	return newRefresh, tok, nil
}
