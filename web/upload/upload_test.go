package upload

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
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
