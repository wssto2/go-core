package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/wssto2/go-core/ratelimit"
)

func TestRateLimit_Global(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lim := ratelimit.NewInMemoryLimiter(2, 120*time.Millisecond)
	r := gin.New()
	r.Use(RateLimit(lim, false, false))

	var calls int
	r.GET("/ping", func(c *gin.Context) {
		calls++
		c.String(http.StatusOK, "pong")
	})

	// two allowed
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ping", nil)
	r.ServeHTTP(w, req)
	req2 := httptest.NewRequest("GET", "/ping", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	req3 := httptest.NewRequest("GET", "/ping", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	require.Equal(t, 2, calls)
	require.Equal(t, http.StatusTooManyRequests, w3.Code)

	// after window expiry it should allow again
	time.Sleep(160 * time.Millisecond)
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest("GET", "/ping", nil)
	r.ServeHTTP(w4, req4)
	require.Equal(t, http.StatusOK, w4.Code)
}

func TestRateLimit_PerUserAndEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// per-user limiter: allow 1 per user
	lim := ratelimit.NewInMemoryLimiter(1, time.Second)
	r := gin.New()
	r.Use(RateLimit(lim, true, true))

	// two endpoints share the same limiter configuration but should be scoped per-endpoint
	var callsA, callsB int
	r.GET("/a", func(c *gin.Context) {
		callsA++
		c.String(http.StatusOK, "a")
	})
	r.GET("/b", func(c *gin.Context) {
		callsB++
		c.String(http.StatusOK, "b")
	})

	// user 1 calls /a -> allowed
	req := httptest.NewRequest("GET", "/a", nil)
	req.Header.Set("X-User-ID", "1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// same user calls /a again -> denied
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/a", nil)
	req2.Header.Set("X-User-ID", "1")
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusTooManyRequests, w2.Code)

	// same user calls /b -> allowed because per-endpoint scoping
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/b", nil)
	req3.Header.Set("X-User-ID", "1")
	r.ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)

	// different user can call /a
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest("GET", "/a", nil)
	req4.Header.Set("X-User-ID", "2")
	r.ServeHTTP(w4, req4)
	require.Equal(t, http.StatusOK, w4.Code)
}
