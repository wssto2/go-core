package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// stub user implements Identifiable for tests
type stubUser struct{ id int }

func (s stubUser) GetID() int { return s.id }

// staticProvider is a tiny test-friendly AuthProvider implementation.
type staticProvider struct {
	expected string
	user     Identifiable
	retErr   error
}

func (p *staticProvider) Verify(ctx context.Context, token string) (Identifiable, error) {
	if p.retErr != nil {
		return nil, p.retErr
	}
	if token == p.expected {
		return p.user, nil
	}
	return nil, ErrUnauthorized
}

func TestMultiProvider_SucceedsWhenSecondProviderMatches(t *testing.T) {
	p1 := &staticProvider{expected: "nope", user: nil}
	p2 := &staticProvider{expected: "good", user: stubUser{id: 7}}
	mp := NewMultiProvider(p1, p2)

	u, err := mp.Verify(context.Background(), "good")
	require.NoError(t, err)
	require.Equal(t, 7, u.GetID())
}

func TestMultiProvider_ReturnsExpiredImmediately(t *testing.T) {
	p1 := &staticProvider{retErr: ErrExpiredToken}
	p2 := &staticProvider{expected: "good", user: stubUser{id: 9}}
	mp := NewMultiProvider(p1, p2)

	_, err := mp.Verify(context.Background(), "anything")
	require.ErrorIs(t, err, ErrExpiredToken)
}

func TestMultiProvider_ReturnsUnauthorizedIfNoneMatch(t *testing.T) {
	p1 := &staticProvider{expected: "a"}
	p2 := &staticProvider{expected: "b"}
	mp := NewMultiProvider(p1, p2)

	_, err := mp.Verify(context.Background(), "c")
	require.ErrorIs(t, err, ErrUnauthorized)
}
