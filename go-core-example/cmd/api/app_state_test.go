package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	coreauth "github.com/wssto2/go-core/auth"
)

type testViewer struct {
	id       int
	email    string
	policies []string
}

func (v testViewer) GetID() int            { return v.id }
func (v testViewer) GetEmail() string      { return v.email }
func (v testViewer) GetPolicies() []string { return v.policies }

func TestAppStateProviderBuild_ResolvesViewerFromBearerToken(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tokenCfg := coreauth.TokenConfig{
		SecretKey:     "change-me-to-32-bytes-minimum!!!",
		Issuer:        "go-core-example",
		TokenDuration: time.Hour,
	}
	token, err := coreauth.IssueToken(coreauth.Claims{
		UserID: 7,
		Email:  "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "7",
		},
	}, tokenCfg)
	require.NoError(t, err)

	provider := newAppStateProvider(loadConfig(), tokenCfg, func(ctx context.Context, id string) (coreauth.Identifiable, error) {
		require.Equal(t, "7", id)
		return testViewer{
			id:       7,
			email:    "admin",
			policies: []string{"products:create"},
		}, nil
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	ctx.Request = req

	state, ok := provider.Build(ctx).(appState)
	require.True(t, ok)
	require.Equal(t, "go-core-example", state.AppName)
	require.Equal(t, "/dashboard", state.Path)
	require.NotNil(t, state.Viewer)
	require.Equal(t, "admin", state.Viewer.Username)
	require.Equal(t, []string{"products:create"}, state.Viewer.Policies)
	require.Empty(t, state.ViewerError)
}

func TestAppStateProviderBuild_SurfacesViewerTokenErrors(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tokenCfg := coreauth.TokenConfig{
		SecretKey:     "change-me-to-32-bytes-minimum!!!",
		Issuer:        "go-core-example",
		TokenDuration: time.Hour,
	}
	provider := newAppStateProvider(loadConfig(), tokenCfg, func(ctx context.Context, id string) (coreauth.Identifiable, error) {
		return testViewer{id: 1}, nil
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	ctx.Request = req

	state, ok := provider.Build(ctx).(appState)
	require.True(t, ok)
	require.Nil(t, state.Viewer)
	require.Equal(t, "viewer token is invalid", state.ViewerError)
}
