package middlewares

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestRequestIDPreservesValidHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderXRequestID, "my-id-123")
	c.Request = req

	RequestID()(c)

	header := w.Header().Get(HeaderXRequestID)
	assert.Equal(t, "my-id-123", header, "expected valid X-Request-ID to be preserved")
	assert.Equal(t, "my-id-123", c.GetString("request_id"), "expected gin context to contain the provided request id")
}

func TestRequestID_CRLFInjection_IsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderXRequestID, "id\r\nX-Injected: evil")
	c.Request = req

	RequestID()(c)

	id := w.Header().Get(HeaderXRequestID)
	require.NotEmpty(t, id)
	assert.False(t, strings.Contains(id, "\r") || strings.Contains(id, "\n"),
		"reflected ID must not contain CRLF")
	assert.NotEqual(t, "id\r\nX-Injected: evil", id, "CRLF value must be discarded")
}

func TestRequestID_TooLong_IsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderXRequestID, strings.Repeat("a", 129))
	c.Request = req

	RequestID()(c)

	id := w.Header().Get(HeaderXRequestID)
	require.NotEmpty(t, id)
	assert.NotEqual(t, strings.Repeat("a", 129), id, "over-long value must be replaced")
}

func TestRequestID_ValidUUID_PassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validID := "550e8400-e29b-41d4-a716-446655440000"
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderXRequestID, validID)
	c.Request = req

	RequestID()(c)

	assert.Equal(t, validID, w.Header().Get(HeaderXRequestID), "valid UUID must pass through unchanged")
}

func TestRequestID_Empty_GeneratesUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/", nil)
	// no X-Request-ID header set
	c.Request = req

	RequestID()(c)

	id := w.Header().Get(HeaderXRequestID)
	require.NotEmpty(t, id, "empty header should generate a UUID")
	assert.Regexp(t, `^[a-fA-F0-9\-]{36}$`, id, "generated ID should be a UUID")
}
