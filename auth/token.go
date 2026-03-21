package auth

import (
	"time"
)

type Token struct {
	ID           int       `json:"id" gorm:"primarykey"`
	UserID       int       `json:"user_id" gorm:"index;not null"`
	TokenValue   string    `json:"token_value" gorm:"size:255;not null;uniqueIndex"`
	Name         string    `json:"name" gorm:"size:255"`
	LastUsedAt   time.Time `json:"last_used_at"`
	LastUsedIP   string    `json:"last_used_ip" gorm:"size:45"`
	ExpiresAt    time.Time `json:"expires_at" gorm:"index;not null"`
	CreatedAt    time.Time `json:"created_at"`
	RefreshToken string    `json:"refresh_token" gorm:"size:255"`
	Revoked      bool      `json:"revoked" gorm:"default:false"`
}

func (t *Token) IsExpired() bool {
	return t.ExpiresAt.Before(time.Now())
}

func (t *Token) IsValid() bool {
	return !t.Revoked && !t.IsExpired()
}
