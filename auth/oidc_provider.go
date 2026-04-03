package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/golang-jwt/jwt/v5"
)

// Minimal JWKS parsing structures
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwks struct {
	Keys []jwkKey `json:"keys"`
}

// OIDCProvider verifies tokens using a remote JWKS endpoint with simple in-memory caching.
// It implements AuthProvider.
type OIDCProvider struct {
	jwksURL  string
	issuer   string
	ttl      time.Duration
	client   *http.Client
	resolver IdentityResolver

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	expiresAt time.Time
	sfGroup   singleflight.Group
}

// NewOIDCProvider creates a provider. ttl controls how long JWKS are cached; pass 0 for 5m default.
func NewOIDCProvider(jwksURL, issuer string, resolver IdentityResolver, ttl time.Duration) *OIDCProvider {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &OIDCProvider{
		jwksURL:  jwksURL,
		issuer:   issuer,
		ttl:      ttl,
		client:   &http.Client{Timeout: 5 * time.Second},
		resolver: resolver,
		keys:     make(map[string]*rsa.PublicKey),
	}
}

// Verify implements AuthProvider.Verify. It validates the token signature via JWKS and then resolves the user.
func (p *OIDCProvider) Verify(ctx context.Context, tokenString string) (Identifiable, error) {
	if tokenString == "" {
		return nil, ErrMissingToken
	}

	claims := &Claims{}

	keyFunc := func(token *jwt.Token) (any, error) {
		// Expect RS256 for OIDC tokens in this minimal implementation.
		if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("missing kid in token header")
		}
		k, err := p.getKey(ctx, kid)
		if err != nil {
			return nil, err
		}
		return k, nil
	}

	token, err := jwt.ParseWithClaims(tokenString, claims, keyFunc)
	if err != nil {
		if isExpiredError(err) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	return resolveFromClaims(ctx, token, claims, p.issuer, p.resolver)
}

// getKey returns the RSA public key for the given kid, refreshing the JWKS cache if necessary.
func (p *OIDCProvider) getKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	p.mu.RLock()
	if time.Now().Before(p.expiresAt) {
		if k, ok := p.keys[kid]; ok {
			p.mu.RUnlock()
			return k, nil
		}
	}
	p.mu.RUnlock()

	// Coalesce all concurrent fetches into one HTTP call.
	// Use a fresh context (not the request context) so a client disconnect does not
	// cancel the shared fetch and cause auth failures for all waiting callers.
	fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err, _ := p.sfGroup.Do("jwks", func() (any, error) {
		return nil, p.fetch(fetchCtx)
	})
	if err != nil {
		return nil, err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	if k, ok := p.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("kid not found: %s", kid)
}

// fetch pulls the JWKS URL and updates the in-memory cache.
func (p *OIDCProvider) fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.jwksURL, nil)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("jwks fetch failed: status=%d", resp.StatusCode)
	}

	var raw jwks
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return err
	}

	m := make(map[string]*rsa.PublicKey)
	for _, k := range raw.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := parseRSAPublicFromJWK(k)
		if err != nil {
			continue
		}
		m[k.Kid] = pub
	}

	p.mu.Lock()
	p.keys = m
	p.expiresAt = time.Now().Add(p.ttl)
	p.mu.Unlock()
	return nil
}

// parseRSAPublicFromJWK converts a JWK RSA key to *rsa.PublicKey.
func parseRSAPublicFromJWK(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		nBytes, err = base64.URLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, fmt.Errorf("decode n: %w", err)
		}
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		eBytes, err = base64.URLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, fmt.Errorf("decode e: %w", err)
		}
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}
	if e == 0 {
		return nil, fmt.Errorf("invalid exponent")
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}
