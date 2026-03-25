package auth

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type gormTokenStore struct {
	db *gorm.DB
}

func NewGormTokenStore(db *gorm.DB) TokenStore {
	return &gormTokenStore{db: db}
}

func (s *gormTokenStore) FindValidToken(ctx context.Context, val string) (*Token, error) {
	var ut Token
	err := s.db.WithContext(ctx).
		Where("token_value = ? AND revoked = ? AND expires_at > ?", val, false, time.Now()).
		First(&ut).Error
	if err != nil {
		return nil, err
	}
	return &ut, nil
}

func (s *gormTokenStore) UpdateTouch(ctx context.Context, id uint64, meta TokenMetadata) error {
	return s.db.Model(&Token{}).Where("id = ?", id).Updates(map[string]any{
		"last_used_at": meta.LastUsedAt,
		"last_used_ip": meta.LastUsedIP,
	}).Error
}
