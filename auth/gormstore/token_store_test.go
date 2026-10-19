package gormstore_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/auth/gormstore"
	"github.com/wssto2/go-core/database"
)

func TestRotateRefreshToken_SuccessAndInvalidOld(t *testing.T) {
	db, cleanup, err := database.PrepareTestDB(&auth.Token{})
	require.NoError(t, err)
	defer cleanup()

	store := gormstore.NewGormTokenStore(db)

	hasher := &auth.BcryptHasher{}
	hashed, _ := hasher.Hash("refresh-old")

	// Create initial token with a known refresh value
	initial := &auth.Token{
		UserID:        1,
		TokenValue:    "tv-1",
		RefreshPrefix: "refresh-old"[:8],
		RefreshToken:  hashed,
		ExpiresAt:     time.Now().Add(time.Hour),
		CreatedAt:     time.Now(),
	}
	require.NoError(t, db.Create(initial).Error)

	// Rotate
	ctx := context.Background()
	newRefresh := "refresh-new-1"
	tok, err := store.FindAndRotateRefreshToken(ctx, "refresh-old", newRefresh, time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, initial.ID, tok.ID)

	// Verify DB updated: should store a hashed value, not the raw token
	var got auth.Token
	require.NoError(t, db.First(&got, initial.ID).Error)
	require.NotEqual(t, newRefresh, got.RefreshToken)
	require.NotEqual(t, "refresh-old", got.RefreshToken)
	require.True(t, strings.HasPrefix(got.RefreshToken, "$2"), "refresh token in DB should be hashed")

	// Using the old refresh token should now fail
	_, err = store.FindAndRotateRefreshToken(ctx, "refresh-old", "refresh-new-2", time.Now().Add(24*time.Hour))
	require.Error(t, err)

	// Using the new refresh token should succeed
	nextTok, err := store.FindAndRotateRefreshToken(ctx, newRefresh, "refresh-new-3", time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, initial.ID, nextTok.ID)
}
