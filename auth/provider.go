package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AuthProvider is the interface for verifying tokens and returning an Identifiable user.
type Provider interface {
	// Verify takes a raw string (from Header/CLI) and returns the User.
	Verify(ctx context.Context, token string) (Identifiable, error)
}

// -- JWTProvider ──────────────────────────────────────────────────────────────

// JWTProvider implements AuthProvider using JSON Web Tokens.
type JWTProvider struct {
	cfg      TokenConfig
	resolver IdentityResolver
}

// NewJWTProvider returns a new JWTProvider.
func NewJWTProvider(cfg TokenConfig, resolver IdentityResolver) *JWTProvider {
	return &JWTProvider{cfg: cfg, resolver: resolver}
}

// Verify validates the token and resolves the user.
func (p *JWTProvider) Verify(ctx context.Context, tokenString string) (Identifiable, error) {
	if tokenString == "" {
		return nil, ErrMissingToken
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		alg := p.cfg.Algorithm
		if alg == "" {
			alg = "HS256"
		}
		switch alg {
		case "HS256":
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(p.cfg.SecretKey), nil
		case "RS256":
			if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			pub, err := parseRSAPublicKeyFromPEM([]byte(p.cfg.RSAPublicKeyPEM))
			if err != nil {
				return nil, fmt.Errorf("parse public key: %w", err)
			}
			return pub, nil
		default:
			return nil, fmt.Errorf("unsupported signing algorithm: %s", alg)
		}
	})
	if err != nil {
		if isExpiredError(err) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	return resolveFromClaims(ctx, token, claims, p.cfg.Issuer, p.resolver)
}

// resolveFromClaims validates a successfully-parsed JWT token and resolves
// the caller identity. It checks token.Valid, optionally validates issuer,
// then delegates to the resolver.
// issuer may be empty string (skips issuer check).
func resolveFromClaims(
	ctx context.Context,
	token *jwt.Token,
	claims *Claims,
	issuer string,
	resolver IdentityResolver,
) (Identifiable, error) {
	if !token.Valid {
		return nil, ErrInvalidClaims
	}
	if issuer != "" && claims.Issuer != issuer {
		return nil, ErrInvalidClaims
	}
	return resolver(ctx, claims.Subject)
}

// -- DBTokenProvider ──────────────────────────────────────────────────────────

// DBTokenProvider implements AuthProvider using database-backed tokens.
type DBTokenProvider struct {
	store    TokenStore
	resolver IdentityResolver
}

// NewDBTokenProvider returns a new DBTokenProvider.
func NewDBTokenProvider(store TokenStore, res IdentityResolver) *DBTokenProvider {
	return &DBTokenProvider{store: store, resolver: res}
}

// Verify validates the token against the store and resolves the user.
func (p *DBTokenProvider) Verify(ctx context.Context, token string) (Identifiable, error) {
	ut, err := p.store.FindValidToken(ctx, token)
	if err != nil {
		return nil, errors.New("unauthorized: invalid or expired token")
	}

	go func() {
		_ = p.store.UpdateTouch(context.Background(), uint64(ut.ID), TokenMetadata{
			LastUsedAt: time.Now(),
		})
	}()

	return p.resolver(ctx, strconv.Itoa(int(ut.UserID)))
}
