package http_test

import (
	nethttp "net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz/authztest"
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
