package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/ratelimit"
)

// tokenProvider maps a bearer token to a user.
type tokenProvider map[string]Identifiable

func (p tokenProvider) Verify(_ context.Context, token string) (Identifiable, error) {
	if u, ok := p[token]; ok {
		return u, nil
	}

	return nil, ErrUnauthorized
}

func newLimitedRouter(limit int) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(DeferUserRateLimit(ratelimit.NewInMemoryLimiter(limit, time.Minute)))
	r.GET("/me", Authenticated(tokenProvider{"a": stubUser{id: 1}, "b": stubUser{id: 2}}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.GET("/public", func(c *gin.Context) { c.Status(http.StatusOK) })

	return r
}

func get(r *gin.Engine, path, token string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	return w.Code
}

// Two users behind the same IP each get their own bucket: the limit is keyed by
// the authenticated user, not the client address.
func TestDeferUserRateLimit_KeysByUserNotIP(t *testing.T) {
	r := newLimitedRouter(1)

	require.Equal(t, http.StatusOK, get(r, "/me", "a"))
	require.Equal(t, http.StatusTooManyRequests, get(r, "/me", "a"))
	require.Equal(t, http.StatusOK, get(r, "/me", "b"), "another user on the same IP is not limited by user a")
}

// Unauthenticated routes and failed authentication are not counted.
func TestDeferUserRateLimit_OnlyAuthenticatedRequestsCount(t *testing.T) {
	r := newLimitedRouter(1)

	// A failed authentication aborts before the limit (the app's error handler
	// writes the 401), so it never spends the user's budget and is never a 429.
	for range 3 {
		require.Equal(t, http.StatusOK, get(r, "/public", ""))
		require.NotEqual(t, http.StatusTooManyRequests, get(r, "/me", "forged"))
	}

	require.Equal(t, http.StatusOK, get(r, "/me", "a"))
}

// Without DeferUserRateLimit, Authenticated does not limit at all.
func TestAuthenticated_NoLimiterArmed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/me", Authenticated(tokenProvider{"a": stubUser{id: 1}}), func(c *gin.Context) { c.Status(http.StatusOK) })

	for range 5 {
		require.Equal(t, http.StatusOK, get(r, "/me", "a"))
	}
}
