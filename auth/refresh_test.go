package auth_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/auth"
)

// mockTokenStore implements auth.TokenStore using an in-memory store
// with a mutex to simulate atomic find+rotate behaviour.
type mockTokenStore struct {
	mu           sync.Mutex
	tokens       map[string]*auth.Token // keyed by refresh token value
	rotateCount  int64
	findCalls    int64
}

func (s *mockTokenStore) FindValidToken(_ context.Context, v string) (*auth.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tokens {
		if t.TokenValue == v {
			return t, nil
		}
	}
	return nil, auth.ErrUnauthorized
}

func (s *mockTokenStore) FindByRefreshToken(_ context.Context, refresh string) (*auth.Token, error) {
	atomic.AddInt64(&s.findCalls, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tokens[refresh]; ok {
		return t, nil
	}
	return nil, auth.ErrUnauthorized
}

func (s *mockTokenStore) UpdateTouch(_ context.Context, _ uint64, _ auth.TokenMetadata) error {
	return nil
}

func (s *mockTokenStore) RotateRefreshToken(_ context.Context, id uint64, newRefresh string, newExpiry time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for old, t := range s.tokens {
		if uint64(t.ID) == id {
			delete(s.tokens, old)
			t.RefreshToken = newRefresh
			t.ExpiresAt = newExpiry
			s.tokens[newRefresh] = t
			atomic.AddInt64(&s.rotateCount, 1)
			return nil
		}
	}
	return auth.ErrUnauthorized
}

// FindAndRotateRefreshToken atomically finds and rotates — only one caller wins.
func (s *mockTokenStore) FindAndRotateRefreshToken(_ context.Context, oldRefresh, newRefresh string, newExpiry time.Time) (*auth.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tokens[oldRefresh]
	if !ok || t.Revoked || t.IsExpired() {
		return nil, auth.ErrUnauthorized
	}
	delete(s.tokens, oldRefresh)
	t.RefreshToken = newRefresh
	t.ExpiresAt = newExpiry
	s.tokens[newRefresh] = t
	atomic.AddInt64(&s.rotateCount, 1)
	cp := *t
	return &cp, nil
}

func TestRotateRefreshToken_ConcurrentRequests_OnlyOneSucceeds(t *testing.T) {
	refreshToken := "original-refresh-token"
	store := &mockTokenStore{
		tokens: map[string]*auth.Token{
			refreshToken: {
				ID:           1,
				UserID:       42,
				RefreshToken: refreshToken,
				ExpiresAt:    time.Now().Add(time.Hour),
			},
		},
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	successes := int64(0)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _, err := auth.RotateRefreshToken(context.Background(), store, refreshToken, time.Hour)
			if err == nil {
				atomic.AddInt64(&successes, 1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(1), successes,
		"exactly one concurrent rotate must succeed; got %d", successes)
	assert.Equal(t, int64(1), atomic.LoadInt64(&store.rotateCount),
		"store rotate must be called exactly once")
}
