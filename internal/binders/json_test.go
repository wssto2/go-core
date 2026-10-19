package binders

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
)

func TestBind_BodyOverTheApplicationsLimit_ReturnsPayloadTooLarge(t *testing.T) {
	// web.BodyLimit wraps the body this way; the read fails once the limit is passed.
	body := http.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(bytes.NewBufferString(`{"key":"`+strings.Repeat("a", 100)+`"}`)), 10)

	req, err := http.NewRequest("POST", "/", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	_, _, parseErr := parseJSON(req)
	require.Error(t, parseErr)

	var appErr *apperr.AppError
	require.ErrorAs(t, parseErr, &appErr)
	assert.Equal(t, apperr.CodePayloadTooLarge, appErr.Code)
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
