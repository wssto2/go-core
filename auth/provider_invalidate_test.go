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

// mapStore serves the tokens in it; deleting one is a revoke.
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
	return nil, errors.New("none")
}
func (s *mapStore) revoke(v string) { s.mu.Lock(); delete(s.tokens, v); s.mu.Unlock() }
func (*mapStore) FindByRefreshToken(context.Context, string) (*auth.Token, error) {
	return nil, errors.New("none")
}
func (*mapStore) UpdateTouch(context.Context, uint64, auth.TokenMetadata) error { return nil }
func (*mapStore) RotateRefreshToken(context.Context, uint64, string, time.Time) error {
	return nil
}
func (*mapStore) FindAndRotateRefreshToken(context.Context, string, string, time.Time) (*auth.Token, error) {
	return nil, errors.New("none")
}

func TestDBTokenProvider_InvalidateUserAndSession(t *testing.T) {
	st := &mapStore{tokens: map[string]*auth.Token{}}
	for i, tk := range []struct {
		v    string
		user int
	}{{"a1", 1}, {"a2", 1}, {"b1", 2}} {
		st.tokens[tk.v] = &auth.Token{ID: i + 1, UserID: tk.user, ExpiresAt: time.Now().Add(time.Hour)}
	}
	p := auth.NewDBTokenProvider(st, func(_ context.Context, id string) (auth.Identifiable, error) {
		n, _ := strconv.Atoi(id)
		return uid(n), nil
	}, nil, auth.WithVerifyCache(time.Minute))
	defer p.Stop()
	ctx := context.Background()
	for _, v := range []string{"a1", "a2", "b1"} {
		if _, err := p.Verify(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	st.revoke("a1")
	st.revoke("a2")
	st.revoke("b1")
	// still cached
	if _, err := p.Verify(ctx, "a1"); err != nil {
		t.Fatal("cache should still serve a1")
	}

	// concurrent verifies must not race the invalidation
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = p.Verify(ctx, "b1")
			}
		}()
	}
	p.InvalidateUser(1)
	wg.Wait()

	for _, v := range []string{"a1", "a2"} {
		if _, err := p.Verify(ctx, v); err == nil {
			t.Fatalf("%s still served after InvalidateUser(1)", v)
		}
	}
	if _, err := p.Verify(ctx, "b1"); err != nil {
		t.Fatal("user 2's entry must stay")
	}
	p.InvalidateSession(3)
	if _, err := p.Verify(ctx, "b1"); err == nil {
		t.Fatal("b1 still served after InvalidateSession(3)")
	}
}
