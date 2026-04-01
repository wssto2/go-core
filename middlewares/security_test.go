package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/wssto2/go-core/middlewares"
)

func TestSecurity_DefaultHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middlewares.Security(false))
	r.GET("/", func(c *gin.Context) {
		nonce, _ := c.Get("nonce")
		s := ""
		if n, ok := nonce.(string); ok {
			s = n
		}
		c.String(http.StatusOK, s)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	nonce := strings.TrimSpace(rec.Body.String())
	require.NotEmpty(t, nonce)

	csp := rec.Header().Get("Content-Security-Policy")
	require.Contains(t, csp, "default-src 'self'")
	require.Contains(t, csp, "script-src")
	require.Contains(t, csp, "nonce-"+nonce)

	require.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
	require.Equal(t, "max-age=31536000; includeSubDomains; preload", rec.Header().Get("Strict-Transport-Security"))
}

func TestSecurity_DevAddsVite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middlewares.Security(true))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	csp := rec.Header().Get("Content-Security-Policy")
	require.Contains(t, csp, "localhost:5173")
	require.Contains(t, csp, "ws://localhost:5173")
}

func TestSecurity_CustomCSP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	custom := "default-src 'none';"
	r.Use(middlewares.Security(false, middlewares.SecurityConfig{ContentSecurityPolicy: custom}))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, custom, rec.Header().Get("Content-Security-Policy"))
}
