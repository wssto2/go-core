package auth

import (
	"context"
	"time"
)

// APIKey represents a user-scoped API key stored hashed in the database.
type APIKey struct {
	ID         int       `json:"id" gorm:"primarykey"`
	UserID     int       `json:"user_id" gorm:"index;not null"`
	KeyPrefix  string    `json:"key_prefix" gorm:"size:8;not null;index"`
	KeyHash    string    `json:"key_hash" gorm:"size:255;not null;uniqueIndex"`
	Name       string    `json:"name" gorm:"size:255"`
	Revoked    bool      `json:"revoked" gorm:"default:false"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// APIKeyStore is the persistence port for API keys.
type APIKeyStore interface {
	FindByKey(ctx context.Context, key string) (*APIKey, error)
	CreateKey(ctx context.Context, key *APIKey, raw string) error
	RevokeKey(ctx context.Context, id int) error
}

// GenerateAPIKey returns a new cryptographically secure API key string.
// It reuses the refresh token generator to keep length/encoding consistent.
func GenerateAPIKey() (string, error) {
	return GenerateRefreshToken()
}

// ValidateAPIKey verifies the provided raw key using the store. On failure
// ErrUnauthorized is returned to align with other auth helpers.
func ValidateAPIKey(ctx context.Context, store APIKeyStore, raw string) (*APIKey, error) {
	ak, err := store.FindByKey(ctx, raw)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if ak.Revoked {
		return nil, ErrUnauthorized
	}
	return ak, nil
}
