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
	newRefresh, tok, err := auth.RotateRefreshToken(context.Background(), store, "refresh-old", 24*time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, newRefresh)
	require.Equal(t, initial.ID, tok.ID)
	require.NotEqual(t, "refresh-old", newRefresh)

	// Verify DB updated: should store a hashed value, not the raw token
	var got auth.Token
	require.NoError(t, db.First(&got, initial.ID).Error)
	require.NotEqual(t, newRefresh, got.RefreshToken)
	require.NotEqual(t, "refresh-old", got.RefreshToken)
	require.True(t, strings.HasPrefix(got.RefreshToken, "$2"), "refresh token in DB should be hashed")

	// Using the old refresh token should now fail
	_, _, err = auth.RotateRefreshToken(context.Background(), store, "refresh-old", 24*time.Hour)
	require.ErrorIs(t, err, auth.ErrUnauthorized)

	// Using the new refresh token should succeed
	nextRefresh, nextTok, err := auth.RotateRefreshToken(context.Background(), store, newRefresh, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, initial.ID, nextTok.ID)
	require.NotEqual(t, newRefresh, nextRefresh)
}
