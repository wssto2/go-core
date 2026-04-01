package gormstore_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/auth/gormstore"
	"github.com/wssto2/go-core/database"
)

func TestAPIKey_CreateFindRevoke(t *testing.T) {
	t.Parallel()

	conn, cleanup, err := database.PrepareTestDB(&auth.APIKey{}) //nolint:exhaustruct
	require.NoError(t, err)

	defer func() { _ = cleanup() }()

	store := gormstore.NewGormAPIKeyStore(conn)

	raw, err := auth.GenerateAPIKey()
	require.NoError(t, err)

	apiKey := &auth.APIKey{UserID: 1, Name: "test-key"} //nolint:exhaustruct
	require.NoError(t, store.CreateKey(context.Background(), apiKey, raw))
	require.NotZero(t, apiKey.ID)

	// DB should store a hashed value, not the plaintext key
	var got auth.APIKey
	require.NoError(t, conn.First(&got, apiKey.ID).Error)
	require.NotEqual(t, raw, got.KeyHash)
	require.True(t, strings.HasPrefix(got.KeyHash, "$2"), "key hash should be bcrypt")

	// Validate via helper
	vk, err := auth.ValidateAPIKey(context.Background(), store, raw)
	require.NoError(t, err)
	require.Equal(t, apiKey.ID, vk.ID)

	// Revoke and ensure validation fails
	require.NoError(t, store.RevokeKey(context.Background(), apiKey.ID))
	_, err = auth.ValidateAPIKey(context.Background(), store, raw)
	require.ErrorIs(t, err, auth.ErrUnauthorized)
}
