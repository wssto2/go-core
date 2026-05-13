package gormstore

import (
	"context"
	"errors"
	"time"

	"github.com/wssto2/go-core/auth"
	"gorm.io/gorm"
)

type TokenStoreOption func(*gormTokenStore)

// WithRefreshHasher overrides the hasher used for refresh token storage and
// verification. Defaults to BcryptHasher. Use HMACHasher for high-entropy
// random tokens to eliminate the bcrypt latency (~80ms per operation).
func WithRefreshHasher(h auth.Hasher) TokenStoreOption {
	return func(s *gormTokenStore) {
		s.refreshHasher = h
		s.dummyHash, _ = h.Hash("$dummy-refresh-token$")
	}
}

type gormTokenStore struct {
	db            *gorm.DB
	refreshHasher auth.Hasher
	dummyHash     string // pre-computed hash for timing-safe "no candidates" path
}

func NewGormTokenStore(db *gorm.DB, opts ...TokenStoreOption) auth.TokenStore {
	defaultHasher := &auth.BcryptHasher{}
	dummyHash, _ := defaultHasher.Hash("$dummy-refresh-token$")
	s := &gormTokenStore{
		db:            db,
		refreshHasher: defaultHasher,
		dummyHash:     dummyHash,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *gormTokenStore) FindValidToken(ctx context.Context, val string) (*auth.Token, error) {
	var ut auth.Token
	err := s.db.WithContext(ctx).
		Where("token_value = ? AND revoked = ? AND expires_at > ?", val, false, time.Now()).
		First(&ut).Error
	if err != nil {
		return nil, err
	}
	return &ut, nil
}

func (s *gormTokenStore) FindByRefreshToken(ctx context.Context, refresh string) (*auth.Token, error) {
	if len(refresh) < 8 {
		return nil, gorm.ErrRecordNotFound
	}
	prefix := refresh[:8]

	var candidates []auth.Token
	if err := s.db.WithContext(ctx).
		Where("refresh_prefix = ? AND revoked = ? AND expires_at > ?", prefix, false, time.Now().UTC()).
		Find(&candidates).Error; err != nil {
		return nil, err
	}

	// Perform a dummy comparison when no candidates exist to normalize timing
	// and prevent attackers from enumerating valid refresh token prefixes.
	if len(candidates) == 0 {
		s.refreshHasher.Compare("$dummy-refresh-token$", s.dummyHash)
		return nil, gorm.ErrRecordNotFound
	}

	// Iterate ALL candidates before returning to avoid early-exit timing leaks.
	var found *auth.Token
	for _, c := range candidates {
		if s.refreshHasher.Compare(refresh, c.RefreshToken) && found == nil {
			found = &c
		}
	}
	if found == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return found, nil
}

func (s *gormTokenStore) UpdateTouch(ctx context.Context, id uint64, meta auth.TokenMetadata) error {
	return s.db.Model(&auth.Token{}).Where("id = ?", id).Updates(map[string]any{
		"last_used_at": meta.LastUsedAt,
		"last_used_ip": meta.LastUsedIP,
	}).Error
}

func (s *gormTokenStore) RotateRefreshToken(ctx context.Context, id uint64, newRefresh string, newExpiry time.Time) error {
	if len(newRefresh) < 8 {
		return errors.New("refresh token too short")
	}
	hashed, err := s.refreshHasher.Hash(newRefresh)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&auth.Token{}).Where("id = ?", id).Updates(map[string]any{
		"refresh_token":  hashed,
		"refresh_prefix": newRefresh[:8], // NEW
		"expires_at":     newExpiry,
	}).Error
}

// FindAndRotateRefreshToken atomically finds the token by oldRefresh and rotates it
// to newRefresh inside a single database transaction. This prevents replay attacks
// where two concurrent requests with the same token both succeed.
func (s *gormTokenStore) FindAndRotateRefreshToken(ctx context.Context, oldRefresh, newRefresh string, newExpiry time.Time) (*auth.Token, error) {
if len(newRefresh) < 8 {
return nil, errors.New("new refresh token too short")
}
hashed, err := s.refreshHasher.Hash(newRefresh)
if err != nil {
return nil, err
}

var result *auth.Token
txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
// Pessimistic lock: prevents concurrent reads of the same token.
// Falls back to standard find for databases that don't support FOR UPDATE (e.g. SQLite).
var token auth.Token
q := tx.Set("gorm:query_option", "FOR UPDATE").
Where("revoked = ? AND expires_at > ?", false, time.Now().UTC())
if len(oldRefresh) >= 8 {
q = q.Where("refresh_prefix = ?", oldRefresh[:8])
}
if err := q.Find(&token).Error; err != nil {
return err
}
if token.ID == 0 {
return auth.ErrUnauthorized
}
if !s.refreshHasher.Compare(oldRefresh, token.RefreshToken) {
return auth.ErrUnauthorized
}
if token.Revoked || token.IsExpired() {
return auth.ErrUnauthorized
}
if err := tx.Model(&auth.Token{}).Where("id = ?", token.ID).Updates(map[string]any{
"refresh_token":  hashed,
"refresh_prefix": newRefresh[:8],
"expires_at":     newExpiry,
}).Error; err != nil {
return err
}
token.RefreshToken = newRefresh
token.ExpiresAt = newExpiry
result = &token
return nil
})
if txErr != nil {
return nil, txErr
}
return result, nil
}
