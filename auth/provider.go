package auth

import (
	"context"
	"errors"
	"strconv"
	"time"
)

type AuthProvider[T Identifiable] interface {
	// Verify takes a raw string (from Header/CLI) and returns the User
	Verify(ctx context.Context, token string) (T, error)
}

// -- JWTProvider ──────────────────────────────────────────────────────────────
type JWTProvider[T Identifiable] struct {
	cfg      TokenConfig
	resolver IdentityResolver[T]
}

func NewJWTProvider[T Identifiable](cfg TokenConfig, resolver IdentityResolver[T]) *JWTProvider[T] {
	return &JWTProvider[T]{cfg: cfg, resolver: resolver}
}

func (p *JWTProvider[T]) Verify(ctx context.Context, token string) (T, error) {
	// TokenConfig.Parse handles jwt.ParseWithClaims and secret validation
	claims, err := ParseToken(token, p.cfg)
	if err != nil {
		return *new(T), err
	}
	return p.resolver(ctx, claims.Subject)
}

// -- DBTokenProvider ──────────────────────────────────────────────────────────
type DBTokenProvider[T Identifiable] struct {
	store    TokenStore
	resolver IdentityResolver[T]
}

func NewDBTokenProvider[T Identifiable](store TokenStore, res IdentityResolver[T]) *DBTokenProvider[T] {
	return &DBTokenProvider[T]{store: store, resolver: res}
}

func (p *DBTokenProvider[T]) Verify(ctx context.Context, token string) (T, error) {
	// 1. Fetch from the abstract store
	ut, err := p.store.FindValidToken(ctx, token)
	if err != nil {
		return *new(T), errors.New("unauthorized: invalid or expired token")
	}

	// 2. Update telemetry via the abstract store
	// We do this in a goroutine or ignore error to not block the user
	go p.store.UpdateTouch(context.Background(), uint64(ut.ID), TokenMetadata{
		LastUsedAt: time.Now(),
	})

	// 3. Resolve the user entity
	return p.resolver(ctx, strconv.Itoa(int(ut.UserID)))
}
