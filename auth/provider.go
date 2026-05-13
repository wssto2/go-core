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
	if err := validateTokenConfig(p.cfg); err != nil {
		return nil, err
	}

	claims := &Claims{}
	parseOpts := []jwt.ParserOption{}
	if p.cfg.Audience != "" {
		parseOpts = append(parseOpts, jwt.WithAudience(p.cfg.Audience))
	}
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
	}, parseOpts...)
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
	if claims.Subject == "" {
		return nil, ErrInvalidClaims
	}
	return resolver(ctx, claims.Subject)
}

// -- DBTokenProvider ──────────────────────────────────────────────────────────

// DBTokenProvider implements AuthProvider using database-backed tokens.
type DBTokenProvider struct {
	store    TokenStore
	resolver IdentityResolver
	pool     workerPool
	cache    *tokenCache // nil = disabled
}

// workerPool is a minimal interface for submitting background jobs,
// satisfied by *worker.Pool.
type workerPool interface {
	Submit(ctx context.Context, job func(context.Context) error) error
}

// DBProviderOption configures a DBTokenProvider.
type DBProviderOption func(*DBTokenProvider)

// WithVerifyCache enables an in-memory cache for token verification results.
// Repeated requests with the same token skip the DB lookup for up to ttl.
// Revoked tokens remain valid in cache until evicted (up to ttl); choose a
// short TTL (e.g. 30s) to bound the staleness window after logout.
func WithVerifyCache(ttl time.Duration) DBProviderOption {
	return func(p *DBTokenProvider) {
		p.cache = newTokenCache(ttl)
	}
}

// NewDBTokenProvider returns a DBTokenProvider that uses pool for bounded
// background UpdateTouch jobs. The pool should be started before the server
// begins serving requests (bootstrap does this automatically via Boot).
func NewDBTokenProvider(store TokenStore, res IdentityResolver, pool workerPool, opts ...DBProviderOption) *DBTokenProvider {
	p := &DBTokenProvider{store: store, resolver: res, pool: pool}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Verify validates the token against the store and resolves the user.
// If a cache is configured and the token was recently validated, the cached
// identity is returned without any DB round-trips.
func (p *DBTokenProvider) Verify(ctx context.Context, token string) (Identifiable, error) {
	if p.cache != nil {
		if e, ok := p.cache.get(token); ok {
			p.touchAsync(e.tokenID)
			return e.user, nil
		}
	}

	ut, err := p.store.FindValidToken(ctx, token)
	if err != nil {
		return nil, errors.New("unauthorized: invalid or expired token")
	}

	p.touchAsync(ut.ID)

	user, err := p.resolver(ctx, strconv.Itoa(int(ut.UserID)))
	if err != nil {
		return nil, err
	}

	if p.cache != nil {
		p.cache.set(token, ut.ID, user, ut.ExpiresAt)
	}

	return user, nil
}

// InvalidateToken removes a token from the verify cache immediately.
// Call this on logout so revoked tokens are not served from cache.
func (p *DBTokenProvider) InvalidateToken(token string) {
	if p.cache != nil {
		p.cache.delete(token)
	}
}

// Stop shuts down the cache's background sweep goroutine, if any.
func (p *DBTokenProvider) Stop() {
	if p.cache != nil {
		p.cache.Stop()
	}
}

// touchAsync updates the token's last-used timestamp in the background.
// If a worker pool is configured, the job is submitted there (bounded).
// If the queue is full, the update is skipped — it is non-critical.
// If no pool is configured, a goroutine is used as a fallback.
func (p *DBTokenProvider) touchAsync(tokenID int) {
	job := func(_ context.Context) error {
		return p.store.UpdateTouch(context.Background(), uint64(tokenID), TokenMetadata{
			LastUsedAt: time.Now(),
		})
	}
	if p.pool != nil {
		if err := p.pool.Submit(context.Background(), job); err != nil {
			// ErrQueueFull: best-effort, skip the update
			return
		}
		return
	}
	go func() { _ = job(context.Background()) }()
}
