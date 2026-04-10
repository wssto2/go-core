package upload

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

const multipartOverheadLimit int64 = 1 << 20

type Config struct {
	MaxSize     int64    // in MB (default 5MB)
	AllowedMime []string // allowed mime types
	IsPhoto     bool     // if true, sets default photo mimes
	StorePath   string   // Relative path inside BaseDir (e.g. "avatars")
	BaseDir     string   // Absolute path to storage root (REQUIRED for UploadFile; not needed for ValidateFile)
}

type UploadedFile struct {
	Name     string
	Path     string // Relative path from BaseDir (or absolute if configured)
	Size     int64
	Ext      string
	MimeType string
}

// FileInput holds a validated, open multipart file ready for further processing.
// Use this with ValidateFile when you want to pass the reader to a custom
// storage backend (S3, GCS, etc.) instead of saving to the local filesystem.
// The caller is responsible for closing File.
type FileInput struct {
	File     io.ReadSeekCloser
	Filename string
	Size     int64
	MimeType string
	Ext      string
}

func resolveAllowedMime(config Config) []string {
	if len(config.AllowedMime) > 0 {
		return config.AllowedMime
	}
	if config.IsPhoto {
		return []string{"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp"}
	}
	return []string{
		"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp",
		"application/pdf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"text/plain",
	}
}

func sanitiseFilename(raw string) string {
	name := filepath.Base(raw)
	if name == "." || name == "" {
		name = "upload"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	sanitised := b.String()
	if sanitised == "" || sanitised == "." {
		return "upload"
	}
	return sanitised
}

func extFromMIME(mimeType, fallbackFilename string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	default:
		return filepath.Ext(fallbackFilename)
	}
}

func UploadFile(ctx *gin.Context, formKey string, config Config) (UploadedFile, error) {
	if config.BaseDir == "" {
		return UploadedFile{}, apperr.BadRequest("upload configuration error: BaseDir is required")
	}
	f, err := ValidateFile(ctx, formKey, config)
	if err != nil {
		return UploadedFile{}, err
	}
	defer func() { _ = f.File.Close() }()

	limitMB := config.MaxSize
	if limitMB <= 0 {
		limitMB = 5
	}
	maxBytes := limitMB * 1024 * 1024

	finalDir := filepath.Join(config.BaseDir, config.StorePath)
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		return UploadedFile{}, apperr.Internal(err)
	}
	timestamp := time.Now().Unix()
	safeName := sanitiseFilename(f.Filename)
	filename := fmt.Sprintf("%d_%s", timestamp, safeName)
	fullPath := filepath.Join(finalDir, filename)
	cleanFull := filepath.Clean(fullPath)
	cleanDir := filepath.Clean(finalDir)
	if !strings.HasPrefix(cleanFull, cleanDir+string(filepath.Separator)) {
		return UploadedFile{}, apperr.BadRequest("upload: resolved path escapes upload directory")
	}
	dst, err := os.Create(fullPath)
	if err != nil {
		return UploadedFile{}, apperr.Internal(err)
	}
	defer func() {
		_ = dst.Close()
		if err != nil {
			_ = os.Remove(fullPath)
		}
	}()
	limitedFile := io.LimitReader(f.File, maxBytes+1)
	written, copyErr := io.Copy(dst, limitedFile)
	if int64(written) > maxBytes {
		_ = os.Remove(fullPath)
		return UploadedFile{}, apperr.BadRequest(fmt.Sprintf("file exceeds %d MB limit", limitMB))
	}
	if copyErr != nil {
		_ = dst.Close()
		_ = os.Remove(fullPath)
		return UploadedFile{}, apperr.Internal(copyErr)
	}
	relativePath := filepath.ToSlash(filepath.Join(config.StorePath, filename))
	return UploadedFile{
		Name:     f.Filename,
		Path:     relativePath,
		Size:     written,
		Ext:      f.Ext,
		MimeType: f.MimeType,
	}, nil
}

// ValidateFile reads and validates the uploaded file from the multipart form
// without saving it to disk. The returned FileInput.File is seeked back to the
// start and ready to read. The caller must close FileInput.File when done.
//
// Use this when the file should be passed to a storage backend (S3, GCS, etc.)
// rather than saved directly to the local filesystem.
func ValidateFile(ctx *gin.Context, formKey string, config Config) (FileInput, error) {
	limitMB := config.MaxSize
	if limitMB <= 0 {
		limitMB = 5
	}
	maxBytes := limitMB * 1024 * 1024

	if ctx.Request == nil || ctx.Request.Body == nil {
		return FileInput{}, apperr.BadRequest("invalid multipart form data")
	}

	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxBytes+multipartOverheadLimit)
	reader, err := ctx.Request.MultipartReader()
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return FileInput{}, apperr.BadRequest(fmt.Sprintf("file size exceeds %dMB limit", limitMB))
		}
		return FileInput{}, apperr.BadRequest("invalid multipart form data")
	}

	for {
		part, err := reader.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return FileInput{}, apperr.BadRequest(fmt.Sprintf("missing file field %q", formKey))
			}
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return FileInput{}, apperr.BadRequest(fmt.Sprintf("file size exceeds %dMB limit", limitMB))
			}
			return FileInput{}, apperr.BadRequest("invalid multipart form data")
		}
		if part.FormName() != formKey || part.FileName() == "" {
			_ = part.Close()
			continue
		}

		tmp, err := os.CreateTemp("", "go-core-upload-*")
		if err != nil {
			_ = part.Close()
			return FileInput{}, apperr.Internal(err)
		}
		cleanup := func() {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			_ = part.Close()
		}

		sniffBuf := make([]byte, 512)
		n, readErr := part.Read(sniffBuf)
		if readErr != nil && readErr != io.EOF {
			cleanup()
			var maxErr *http.MaxBytesError
			if errors.As(readErr, &maxErr) {
				return FileInput{}, apperr.BadRequest(fmt.Sprintf("file size exceeds %dMB limit", limitMB))
			}
			return FileInput{}, apperr.Internal(readErr)
		}
		total := int64(n)
		if total > 0 {
			if _, err := tmp.Write(sniffBuf[:n]); err != nil {
				cleanup()
				return FileInput{}, apperr.Internal(err)
			}
		}
		if total > maxBytes {
			cleanup()
			return FileInput{}, apperr.BadRequest(fmt.Sprintf("file size exceeds %dMB limit", limitMB))
		}

		written, copyErr := io.Copy(tmp, io.LimitReader(part, maxBytes-total+1))
		total += written
		if total > maxBytes {
			cleanup()
			return FileInput{}, apperr.BadRequest(fmt.Sprintf("file size exceeds %dMB limit", limitMB))
		}
		if copyErr != nil {
			cleanup()
			var maxErr *http.MaxBytesError
			if errors.As(copyErr, &maxErr) {
				return FileInput{}, apperr.BadRequest(fmt.Sprintf("file size exceeds %dMB limit", limitMB))
			}
			return FileInput{}, apperr.Internal(copyErr)
		}

		sniffed := http.DetectContentType(sniffBuf[:n])
		if !slices.Contains(resolveAllowedMime(config), sniffed) {
			cleanup()
			return FileInput{}, apperr.BadRequest(fmt.Sprintf("file type '%s' is not allowed", sniffed))
		}
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			cleanup()
			return FileInput{}, apperr.Internal(err)
		}

		return FileInput{
			File:     &tempReadSeekCloser{File: tmp},
			Filename: part.FileName(),
			Size:     total,
			MimeType: sniffed,
			Ext:      extFromMIME(sniffed, part.FileName()),
		}, nil
	}
}

type tempReadSeekCloser struct {
	*os.File
}

func (t *tempReadSeekCloser) Close() error {
	name := t.Name()
	err := t.File.Close()
	if removeErr := os.Remove(name); removeErr != nil && !os.IsNotExist(removeErr) && err == nil {
		err = removeErr
	}
	return err
}

func DeleteFile(basePath, relativePath string) error {
	cleanBase := filepath.Clean(basePath)
	fullPath := filepath.Join(cleanBase, relativePath)
	cleanFull := filepath.Clean(fullPath)
	if !strings.HasPrefix(cleanFull, cleanBase+string(filepath.Separator)) {
		return apperr.BadRequest("invalid file path: directory traversal detected")
	}
	if _, err := os.Stat(cleanFull); os.IsNotExist(err) {
		return apperr.NotFound("file not found")
	}
	return os.Remove(cleanFull)
}
