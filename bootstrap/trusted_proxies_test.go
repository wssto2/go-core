package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDefaultTrustedProxies_DoNotTrustForwardedHeaders(t *testing.T) {
	cfg := DefaultConfig()
	app, err := New(cfg).DefaultInfrastructure().Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	app.engine.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	w := httptest.NewRecorder()

	app.engine.ServeHTTP(w, req)
	if got := w.Body.String(); got != "127.0.0.1" {
		t.Fatalf("ClientIP() trusted forwarded header by default: got %q", got)
	}
}

func TestWithTrustedProxies_AllowsOptInForwardedHeaders(t *testing.T) {
	cfg := DefaultConfig()
	app, err := New(cfg).WithTrustedProxies("127.0.0.1").DefaultInfrastructure().Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	app.engine.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	w := httptest.NewRecorder()

	app.engine.ServeHTTP(w, req)
	if got := w.Body.String(); got != "8.8.8.8" {
		t.Fatalf("ClientIP() did not honor trusted proxy header: got %q", got)
	}
}
