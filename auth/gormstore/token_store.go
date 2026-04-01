package gormstore

import (
	"context"
	"errors"
	"time"

	"github.com/wssto2/go-core/auth"
	"gorm.io/gorm"
)

type gormTokenStore struct {
	db *gorm.DB
}

func NewGormTokenStore(db *gorm.DB) auth.TokenStore {
	return &gormTokenStore{db: db}
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

	for _, c := range candidates {
		if (&auth.BcryptHasher{}).Compare(refresh, c.RefreshToken) {
			cp := c
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
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
	hashed, err := (&auth.BcryptHasher{}).Hash(newRefresh)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&auth.Token{}).Where("id = ?", id).Updates(map[string]any{
		"refresh_token":  hashed,
		"refresh_prefix": newRefresh[:8], // NEW
		"expires_at":     newExpiry,
	}).Error
}
