//nolint:exhaustruct,exhaustruct_v5,lll,varnamelen // tests in the style of the package's others
package http_test

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/serviceaccount"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/ratelimit"
)

// The per-user limit the application armed counts the signed-in account on identity's chain, one bucket per account.
func TestAuthenticationAppliesThePerUserRateLimit(t *testing.T) {
	t.Parallel()

	armed := []gin.HandlerFunc{auth.DeferUserRateLimit(ratelimit.NewInMemoryLimiter(2, time.Minute))}
	h := newHarnessUsing(t, "", authztest.AllowAll(), armed, nil)

	ana := h.as("ana", secret)
	boris := h.as("boris", "hunter2")

	for range 2 {
		require.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodGet, profilePath, nil, ana).Code)
	}

	assert.Equal(t, nethttp.StatusTooManyRequests, h.do(nethttp.MethodGet, profilePath, nil, ana).Code, "the request after the limit")
	assert.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodGet, profilePath, nil, boris).Code, "another account has its own bucket")
}

// fakeKeys knows the keys "k1" and "k2": accounts 1 and 2.
type fakeKeys struct{}

func (fakeKeys) Authenticate(_ context.Context, secret string) (serviceaccount.Caller, error) {
	switch secret {
	case "k1":
		return serviceaccount.Caller{AccountID: 1, KeyID: 11, Name: "one"}, nil
	case "k2":
		return serviceaccount.Caller{AccountID: 2, KeyID: 22, Name: "two"}, nil
	}

	return serviceaccount.Caller{}, apperr.Unauthorized("invalid API key")
}

// A key is counted under its service account, not under a user id every account shares: each has its own bucket.
func TestAKeyRequestIsRateLimitedPerServiceAccount(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil)

	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, true), auth.DeferUserRateLimit(ratelimit.NewInMemoryLimiter(2, time.Minute)))
	engine.GET("/x", identityhttp.Authenticator{SignIn: h.kit.SignIn, Keys: &identityhttp.Keys{Auth: fakeKeys{}, Clock: h.kit.Clock}}.Middleware(),
		func(c *gin.Context) {
			p, _ := authz.PrincipalFrom(c.Request.Context())
			c.String(nethttp.StatusOK, "%s:%d key header: %q", p.Kind, p.ID, c.GetHeader(identityhttp.KeyHeader))
		})

	call := func(secret string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/x", nil)
		req.Header.Set(identityhttp.KeyHeader, secret)

		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		return rec
	}

	first := call("k1")
	require.Equal(t, nethttp.StatusOK, first.Code)
	assert.Equal(t, `service:1 key header: ""`, first.Body.String(), "the principal is the service account, and the secret is gone from the request")
	require.Equal(t, nethttp.StatusOK, call("k1").Code)
	assert.Equal(t, nethttp.StatusTooManyRequests, call("k1").Code, "the request after the limit")
	assert.Equal(t, nethttp.StatusOK, call("k2").Code, "another account has its own bucket")
	assert.Equal(t, nethttp.StatusUnauthorized, call("nope").Code)
}
