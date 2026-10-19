package binders

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
)

func TestBind_OversizedBody_ReturnsBadRequest(t *testing.T) {
	// Build a body that is one byte larger than the limit.
	// The body must be valid JSON prefix to ensure the limit check triggers before JSON parsing.
	// We use a string of maxBodyBytes+1 'a' characters wrapped in quotes (not valid JSON by itself,
	// but large enough to trip the limit before JSON unmarshalling).
	oversized := strings.Repeat("a", int(maxBodyBytes)+1)
	body := io.NopCloser(bytes.NewBufferString(`"` + oversized + `"`))

	req, err := http.NewRequest("POST", "/", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	_, _, parseErr := parseJSON(req)
	require.Error(t, parseErr)

	var appErr *apperr.AppError
	require.ErrorAs(t, parseErr, &appErr)
	assert.Equal(t, apperr.CodeBadRequest, appErr.Code)
	assert.Contains(t, appErr.Message, "too large")
}

func TestBind_NormalBody_Succeeds(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`{"key":"value"}`))

	req, err := http.NewRequest("POST", "/", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	raw, isMultipart, parseErr := parseJSON(req)
	require.NoError(t, parseErr)
	assert.False(t, isMultipart)
	assert.Equal(t, "value", raw["key"])
}
