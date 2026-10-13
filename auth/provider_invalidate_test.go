//nolint:exhaustruct,exhaustruct_v5,varnamelen,noinlineerr,cyclop,funlen // a small fake store and a few tokens
package auth_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/wssto2/go-core/auth"
)

type uid int

func (u uid) GetID() int { return int(u) }

var errNone = errors.New("no such token")

// mapStore serves the tokens in it; revoke removes one.
type mapStore struct {
	mu     sync.Mutex
	tokens map[string]*auth.Token
}

func (s *mapStore) FindValidToken(_ context.Context, v string) (*auth.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if t, ok := s.tokens[v]; ok {
		return t, nil
	}

	return nil, errNone
}

func (*mapStore) FindByRefreshToken(context.Context, string) (*auth.Token, error) {
	return nil, errNone
}

func (*mapStore) UpdateTouch(context.Context, uint64, auth.TokenMetadata) error { return nil }

func (*mapStore) RotateRefreshToken(context.Context, uint64, string, time.Time) error { return nil }

func (*mapStore) FindAndRotateRefreshToken(context.Context, string, string, time.Time) (*auth.Token, error) {
	return nil, errNone
}

func (s *mapStore) revoke(v string) {
	s.mu.Lock()
	delete(s.tokens, v)
	s.mu.Unlock()
}

func TestDBTokenProvider_InvalidateUserAndSession(t *testing.T) {
	t.Parallel()

	store := &mapStore{tokens: map[string]*auth.Token{}}

	for i, tk := range []struct {
		value string
		user  int
	}{{"a1", 1}, {"a2", 1}, {"b1", 2}} {
		store.tokens[tk.value] = &auth.Token{ID: i + 1, UserID: tk.user, ExpiresAt: time.Now().Add(time.Hour)}
	}

	provider := auth.NewDBTokenProvider(store, func(_ context.Context, id string) (auth.Identifiable, error) {
		n, _ := strconv.Atoi(id)

		return uid(n), nil
	}, nil, auth.WithVerifyCache(time.Minute))
	defer provider.Stop()

	ctx := t.Context()

	for _, v := range []string{"a1", "a2", "b1"} {
		if _, err := provider.Verify(ctx, v); err != nil {
			t.Fatal(err)
		}
	}

	// revoked in the store, still cached
	store.revoke("a1")
	store.revoke("a2")
	store.revoke("b1")

	if _, err := provider.Verify(ctx, "a1"); err != nil {
		t.Fatal("the cache should still serve a1")
	}

	// Verify calls run beside the invalidation (the race detector watches)
	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			for range 100 {
				_, _ = provider.Verify(ctx, "b1")
			}
		})
	}

	provider.InvalidateUser(1)
	wg.Wait()

	for _, v := range []string{"a1", "a2"} {
		if _, err := provider.Verify(ctx, v); err == nil {
			t.Fatalf("%s is still served after InvalidateUser(1)", v)
		}
	}

	if _, err := provider.Verify(ctx, "b1"); err != nil {
		t.Fatal("user 2's entry must stay")
	}

	provider.InvalidateSession(3)

	if _, err := provider.Verify(ctx, "b1"); err == nil {
		t.Fatal("b1 is still served after InvalidateSession(3)")
	}
}
