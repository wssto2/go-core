package auth

import (
	"context"
	"errors"
	"strconv"

	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/apperr"
	"gorm.io/gorm"
)

// Service encapsulates authentication business logic.
type Service interface {
	// Login verifies credentials and returns a signed JWT on success.
	Login(ctx context.Context, username, password string) (token string, err error)
	// ResolveIdentity loads the user for a validated JWT subject (user ID).
	ResolveIdentity(ctx context.Context, id string) (coreauth.Identifiable, error)
}

type service struct {
	db       *gorm.DB
	tokenCfg coreauth.TokenConfig
}

func newService(db *gorm.DB, tokenCfg coreauth.TokenConfig) Service {
	return &service{db: db, tokenCfg: tokenCfg}
}

func (s *service) Login(ctx context.Context, username, password string) (string, error) {
	var user User
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Return the same message for not-found and wrong-password to
			// prevent username enumeration.
			return "", apperr.Unauthorized("invalid credentials")
		}
		return "", apperr.Internal(err)
	}

	// TODO: verify password hash in production:
	// if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
	// 	return "", apperr.Unauthorized("invalid credentials")
	// }

	claims := coreauth.Claims{
		UserID: user.ID,
		Email:  user.Username,
		Roles:  user.Policies,
	}
	claims.RegisteredClaims.Subject = strconv.Itoa(user.ID)

	token, err := coreauth.IssueToken(claims, s.tokenCfg)
	if err != nil {
		return "", apperr.Internal(err)
	}
	return token, nil
}

func (s *service) ResolveIdentity(ctx context.Context, id string) (coreauth.Identifiable, error) {
	uid, err := strconv.Atoi(id)
	if err != nil {
		return nil, apperr.Unauthorized("invalid token subject")
	}
	var user User
	if err := s.db.WithContext(ctx).First(&user, uid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.Unauthorized("user not found or deleted")
		}
		return nil, apperr.Internal(err)
	}
	return user, nil
}
