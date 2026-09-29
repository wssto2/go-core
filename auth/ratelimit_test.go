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

// keyedUser is an identity with its own rate-limit key (a service account).
type keyedUser struct {
	stubUser
	key string
}

func (u keyedUser) RateLimitKey() string { return u.key }

// Two identities that share a user ID (service accounts, ID 0) but name their
// own keys get separate buckets.
func TestDeferUserRateLimit_KeysByRateLimitKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(DeferUserRateLimit(ratelimit.NewInMemoryLimiter(1, time.Minute)))
	r.GET("/me", Authenticated(tokenProvider{
		"s1": keyedUser{key: "service:1"},
		"s2": keyedUser{key: "service:2"},
	}), func(c *gin.Context) { c.Status(http.StatusOK) })

	require.Equal(t, http.StatusOK, get(r, "/me", "s1"))
	require.Equal(t, http.StatusTooManyRequests, get(r, "/me", "s1"))
	require.Equal(t, http.StatusOK, get(r, "/me", "s2"), "another service account with the same user ID is not limited by the first")
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
