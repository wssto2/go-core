package upload

import (
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

type Config struct {
	MaxSize     int64    // in MB (default 5MB)
	AllowedMime []string // allowed mime types
	IsPhoto     bool     // if true, sets default photo mimes
	StorePath   string   // Relative path inside BaseDir (e.g. "avatars")
	BaseDir     string   // Absolute path to storage root (REQUIRED)
}

type UploadedFile struct {
	Name     string
	Path     string // Relative path from BaseDir (or absolute if configured)
	Size     int64
	Ext      string
	MimeType string
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
	file, header, err := ctx.Request.FormFile(formKey)
	if err != nil {
		return UploadedFile{}, apperr.Internal(err)
	}
	defer func() { _ = file.Close() }()
	limitMB := config.MaxSize
	if limitMB <= 0 {
		limitMB = 5
	}
	maxBytes := limitMB * 1024 * 1024
	if header.Size > maxBytes {
		return UploadedFile{}, apperr.BadRequest(fmt.Sprintf("file size exceeds %dMB limit", limitMB))
	}
	allowedMime := config.AllowedMime
	if len(allowedMime) == 0 {
		if config.IsPhoto {
			allowedMime = []string{"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp"}
		} else {
			allowedMime = []string{
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
	}
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return UploadedFile{}, apperr.Internal(err)
	}
	sniffed := http.DetectContentType(buf[:n])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return UploadedFile{}, apperr.Internal(err)
	}
	if !slices.Contains(allowedMime, sniffed) {
		return UploadedFile{}, apperr.BadRequest(fmt.Sprintf("file type '%s' is not allowed", sniffed))
	}
	finalDir := filepath.Join(config.BaseDir, config.StorePath)
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		return UploadedFile{}, apperr.Internal(err)
	}
	timestamp := time.Now().Unix()
	safeName := sanitiseFilename(header.Filename)
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
	limitedFile := io.LimitReader(file, maxBytes+1)
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
	relativePath := filepath.Join(config.StorePath, filename)
	relativePath = filepath.ToSlash(relativePath)
	return UploadedFile{
		Name:     header.Filename,
		Path:     relativePath,
		Size:     written,
		Ext:      extFromMIME(sniffed, safeName),
		MimeType: sniffed,
	}, nil
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
