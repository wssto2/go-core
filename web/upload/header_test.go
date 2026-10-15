package upload_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/web/upload"
)

// pngBytes is the 8-byte PNG signature plus an IHDR chunk, enough for content sniffing.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func headerOf(t *testing.T, name string, content []byte) *multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, req.ParseMultipartForm(1<<20))

	return req.MultipartForm.File["file"][0]
}

func TestValidateHeaderAcceptsAnAllowedTypeAndRewinds(t *testing.T) {
	t.Parallel()

	file, mime, err := upload.ValidateHeader(headerOf(t, "a.txt", pngBytes), 1<<20, map[string]bool{"image/png": true})
	require.NoError(t, err)

	defer file.Close()

	assert.Equal(t, "image/png", mime, "the content decides, not the file name")

	got, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, pngBytes, got, "the caller reads from the start")
}

func TestValidateHeaderRefusals(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		header  *multipart.FileHeader
		max     int64
		allowed map[string]bool
		message string
	}{
		"too big":     {headerOf(t, "a.png", pngBytes), 10, map[string]bool{"image/png": true}, "file size exceeds 0 MB"},
		"not allowed": {headerOf(t, "a.png", pngBytes), 1 << 20, map[string]bool{"application/pdf": true}, "unsupported file type: image/png"},
	}

	for name, c := range cases {
		file, mime, err := upload.ValidateHeader(c.header, c.max, c.allowed)

		var app *apperr.AppError
		require.ErrorAs(t, err, &app, name)
		assert.Equal(t, apperr.CodeBadRequest, app.Code, name)
		assert.Equal(t, c.message, app.Message, name)
		assert.Nil(t, file, name)
		assert.Empty(t, mime, name)
	}
}
