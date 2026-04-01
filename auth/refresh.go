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

// RotateRefreshToken finds the token by the provided refresh value, validates it,
// generates a new refresh token, updates the store, and returns the new token.
// If the provided refresh token is invalid, expired, or revoked, ErrUnauthorized
// is returned.
func RotateRefreshToken(ctx context.Context, store TokenStore, oldRefresh string, newTTL time.Duration) (string, *Token, error) {
	tok, err := store.FindByRefreshToken(ctx, oldRefresh)
	if err != nil {
		return "", nil, ErrUnauthorized
	}

	if tok.Revoked || tok.IsExpired() {
		return "", nil, ErrUnauthorized
	}

	newRefresh, err := GenerateRefreshToken()
	if err != nil {
		return "", nil, err
	}

	newExpiry := time.Now().Add(newTTL)
	if err := store.RotateRefreshToken(ctx, uint64(tok.ID), newRefresh, newExpiry); err != nil {
		return "", nil, err
	}

	// Update local copy
	tok.RefreshToken = newRefresh
	tok.ExpiresAt = newExpiry

	return newRefresh, tok, nil
}
