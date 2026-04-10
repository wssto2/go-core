package upload

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

func TestUploadFile_OversizeFile_ReturnsBadRequestAppError(t *testing.T) {
	dir := t.TempDir()

	// Build a multipart request with content larger than 1MB limit
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "big.txt")
	// Write 2MB of data
	chunk := make([]byte, 1024)
	for i := 0; i < 2048; i++ {
		_, err := part.Write(chunk)
		if err != nil {
			t.Fatal(err)
		}
	}
	err := writer.Close()
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	config := Config{BaseDir: dir, MaxSize: 1}
	_, err = UploadFile(ctx, "file", config)
	if err == nil {
		t.Fatal("expected error for oversize file, got nil")
	}
	appErr, ok := err.(*apperr.AppError)
	if !ok || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected BadRequest AppError, got %v", err)
	}
}

// pngHeader is the minimal bytes needed for http.DetectContentType to identify PNG.
var pngHeader = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1 px
}

func buildPNGRequest(t *testing.T, fieldName string, content []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile(fieldName, "test.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(content)
	_ = w.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = req
	return ctx, rec
}

func TestUpload_Size_IsActualBytes(t *testing.T) {
	dir := t.TempDir()
	// Build a payload: PNG header + some extra bytes.
	payload := make([]byte, len(pngHeader)+100)
	copy(payload, pngHeader)

	ctx, _ := buildPNGRequest(t, "file", payload)
	result, err := UploadFile(ctx, "file", Config{
		BaseDir:     dir,
		AllowedMime: []string{"image/png"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Size must reflect actual bytes written, not the client-reported header size.
	if result.Size != int64(len(payload)) {
		t.Errorf("Size = %d; want %d (actual bytes written)", result.Size, len(payload))
	}
}

func TestValidateFile_MissingField_ReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	_, err := ValidateFile(ctx, "file", Config{})
	if err == nil {
		t.Fatal("expected missing file error")
	}

	appErr, ok := err.(*apperr.AppError)
	if !ok || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected BadRequest AppError, got %v", err)
	}
}

func TestValidateFile_TemporaryFileIsRemovedOnClose(t *testing.T) {
	ctx, _ := buildPNGRequest(t, "file", pngHeader)

	file, err := ValidateFile(ctx, "file", Config{AllowedMime: []string{"image/png"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmp, ok := file.File.(*tempReadSeekCloser)
	if !ok {
		t.Fatalf("expected tempReadSeekCloser, got %T", file.File)
	}
	name := tmp.Name()
	if err := file.File.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected temporary file to be removed, got err=%v", err)
	}
}
