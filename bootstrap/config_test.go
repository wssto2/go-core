package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/auth"
)

type authTestUser struct{}

func (authTestUser) GetID() int { return 1 }

func authTestResolver(_ context.Context, _ string) (auth.Identifiable, error) {
	return authTestUser{}, nil
}

func TestLoadConfig_EmptyJWTSecret_DoesNotFailWhenJWTAuthIsUnused(t *testing.T) {
	cfg := DefaultConfig()
	err := LoadConfig(&cfg)
	require.NoError(t, err)
}

func TestWithJWTAuth_EmptyJWTSecret_FailsBuild(t *testing.T) {
	cfg := DefaultConfig()

	_, err := New(cfg).WithJWTAuth(func(ctx context.Context, id string) (auth.Identifiable, error) {
		return authTestResolver(ctx, id)
	}).Build()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET must not be empty")
}

func TestWithJWTAuth_ShortJWTSecret_FailsBuild(t *testing.T) {
	cfg := DefaultConfig()
	cfg.JWT.Secret = "too-short"

	_, err := New(cfg).WithJWTAuth(func(ctx context.Context, id string) (auth.Identifiable, error) {
		return authTestResolver(ctx, id)
	}).Build()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 32 characters")
}

func TestWithJWTAuth_NilResolver_FailsBuild(t *testing.T) {
	cfg := DefaultConfig()
	cfg.JWT.Secret = "0123456789abcdef0123456789abcdef"

	_, err := New(cfg).WithJWTAuth(nil).Build()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "identity resolver must not be nil")
}
