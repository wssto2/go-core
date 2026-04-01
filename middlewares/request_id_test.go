package middlewares

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRequestIDGeneratesAndPropagates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	c.Request = req

	RequestID()(c)

	header := w.Header().Get(HeaderXRequestID)
	assert.NotEmpty(t, header, "expected X-Request-ID header to be set")

	reqID := c.GetString("request_id")
	assert.NotEmpty(t, reqID, "expected gin context to contain request_id")
	assert.Equal(t, header, reqID, "header and gin context request_id should match")

	// ensure request.Context was replaced (injected) by middleware
	assert.NotEqual(t, req.Context(), c.Request.Context(), "expected request.Context() to be replaced with context containing request id")
}

func TestRequestIDPreservesExistingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderXRequestID, "my-id-123")
	c.Request = req

	RequestID()(c)

	header := w.Header().Get(HeaderXRequestID)
	assert.Equal(t, "my-id-123", header, "expected existing X-Request-ID to be preserved")
	assert.Equal(t, "my-id-123", c.GetString("request_id"), "expected gin context to contain the provided request id")
}
