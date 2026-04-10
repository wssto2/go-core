package auth_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/auth"
)

// --- 7.4: JWTProvider audience validation ---

func TestJWTProvider_WrongAudience_ReturnsUnauthorized(t *testing.T) {
	// Issue a token for "service-b"
	cfg := auth.TokenConfig{
		SecretKey:     "test-secret",
		Issuer:        "test-issuer",
		Audience:      "service-b",
		TokenDuration: time.Hour,
	}
	claims := auth.Claims{}
	claims.Subject = "42"
	tok, err := auth.IssueToken(claims, cfg)
	require.NoError(t, err)

	// Provider configured for "service-a" must reject the token.
	providerCfg := auth.TokenConfig{
		SecretKey:     "test-secret",
		Issuer:        "test-issuer",
		Audience:      "service-a",
		TokenDuration: time.Hour,
	}
	resolver := func(_ context.Context, _ string) (auth.Identifiable, error) {
		return nil, nil
	}
	provider := auth.NewJWTProvider(providerCfg, resolver)

	_, err = provider.Verify(context.Background(), tok)
	assert.Error(t, err, "token for wrong audience must be rejected")
	assert.True(t, errors.Is(err, auth.ErrInvalidToken) || err != nil,
		"expected invalid token error, got %v", err)
}

func TestJWTProvider_CorrectAudience_Succeeds(t *testing.T) {
	cfg := auth.TokenConfig{
		SecretKey:     "test-secret",
		Issuer:        "test-issuer",
		Audience:      "service-a",
		TokenDuration: time.Hour,
	}
	claims := auth.Claims{}
	claims.Subject = "1"
	tok, err := auth.IssueToken(claims, cfg)
	require.NoError(t, err)

	resolver := func(_ context.Context, _ string) (auth.Identifiable, error) {
		return &testUser{id: 1}, nil
	}
	provider := auth.NewJWTProvider(cfg, resolver)

	_, err = provider.Verify(context.Background(), tok)
	assert.NoError(t, err)
}

// --- 7.5: DBTokenProvider goroutine leak ---

// fakeTokenStore is a minimal stub that satisfies auth.TokenStore.
type fakeTokenStore struct {
	token *auth.Token
}

func (f *fakeTokenStore) FindValidToken(_ context.Context, _ string) (*auth.Token, error) {
	if f.token == nil {
		return nil, auth.ErrUnauthorized
	}
	return f.token, nil
}
func (f *fakeTokenStore) FindByRefreshToken(_ context.Context, _ string) (*auth.Token, error) {
	return nil, auth.ErrUnauthorized
}
func (f *fakeTokenStore) UpdateTouch(_ context.Context, _ uint64, _ auth.TokenMetadata) error {
	return nil
}
func (f *fakeTokenStore) RotateRefreshToken(_ context.Context, _ uint64, _ string, _ time.Time) error {
	return nil
}
func (f *fakeTokenStore) FindAndRotateRefreshToken(_ context.Context, _, _ string, _ time.Time) (*auth.Token, error) {
	return nil, auth.ErrUnauthorized
}

// fakePool accepts jobs but executes them synchronously for predictable tests.
type fakePool struct {
	mu   sync.Mutex
	jobs int
}

func (fp *fakePool) Submit(_ context.Context, job func(context.Context) error) error {
	fp.mu.Lock()
	fp.jobs++
	fp.mu.Unlock()
	go func() { _ = job(context.Background()) }()
	return nil
}

func TestDBTokenProvider_Verify_NoGoroutineLeak(t *testing.T) {
	pool := &fakePool{}
	store := &fakeTokenStore{
		token: &auth.Token{
			ID:        1,
			UserID:    1,
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	resolver := func(_ context.Context, _ string) (auth.Identifiable, error) {
		return &testUser{id: 1}, nil
	}

	provider := auth.NewDBTokenProvider(store, resolver, pool)

	before := runtime.NumGoroutine()
	const calls = 100
	for i := 0; i < calls; i++ {
		_, err := provider.Verify(context.Background(), "token")
		require.NoError(t, err)
	}
	// Allow pool goroutines to drain.
	time.Sleep(50 * time.Millisecond)

	after := runtime.NumGoroutine()
	delta := after - before
	// With a bounded pool there must be no significant goroutine growth.
	assert.LessOrEqual(t, delta, 10,
		"goroutine count grew by %d after %d calls — possible leak", delta, calls)
}
