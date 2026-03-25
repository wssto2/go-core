package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

// UploadConfig configures the file upload behavior.
type UploadConfig struct {
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

// sanitiseFilename strips any directory component and dangerous characters from
// a user-supplied upload filename, returning a clean basename only.
// It never returns an empty string — falls back to "upload" if the name is blank
// after stripping.
func sanitiseFilename(raw string) string {
	// Strip any directory component the client may have included.
	// filepath.Base handles both "/" and "\" separators.
	name := filepath.Base(raw)

	// filepath.Base returns "." for empty or all-separator input.
	if name == "." || name == "" {
		name = "upload"
	}

	// Replace characters that are problematic on any OS or in URLs.
	// Allow only alphanumerics, dots, hyphens, and underscores.
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

// extFromMIME maps a sniffed MIME type to a canonical file extension.
// Falls back to the sanitised filename's extension if the MIME type is unknown.
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

// UploadFile reads the named form field from the request, validates the file
// size and MIME type, sanitises the filename, and writes the file to disk.
func UploadFile(ctx *gin.Context, formKey string, config UploadConfig) (UploadedFile, error) {
	if config.BaseDir == "" {
		return UploadedFile{}, fmt.Errorf("upload configuration error: BaseDir is required")
	}

	// Get file from request
	file, header, err := ctx.Request.FormFile(formKey)
	if err != nil {
		return UploadedFile{}, err
	}
	defer file.Close()

	// Default size limit: 5MB
	limitMB := config.MaxSize
	if limitMB <= 0 {
		limitMB = 5
	}
	maxBytes := limitMB * 1024 * 1024

	if header.Size > maxBytes {
		return UploadedFile{}, fmt.Errorf("file size %d exceeds limit of %d bytes", header.Size, maxBytes)
	}

	// MIME Validation
	allowedMime := config.AllowedMime
	if len(allowedMime) == 0 {
		if config.IsPhoto {
			allowedMime = []string{"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp"}
		} else {
			// Default broader list
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

	// Read first 512 bytes for MIME sniffing
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return UploadedFile{}, fmt.Errorf("failed to read file: %w", err)
	}
	sniffed := http.DetectContentType(buf[:n])

	// Reset file position for the actual copy
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return UploadedFile{}, fmt.Errorf("failed to seek file: %w", err)
	}

	if !slices.Contains(allowedMime, sniffed) {
		return UploadedFile{}, fmt.Errorf("file type '%s' is not allowed", sniffed)
	}

	// Prepare directory
	finalDir := filepath.Join(config.BaseDir, config.StorePath)
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		return UploadedFile{}, fmt.Errorf("failed to create upload directory: %w", err)
	}

	// Generate filename: timestamp_safe-original-name
	timestamp := time.Now().Unix()
	safeName := sanitiseFilename(header.Filename)
	filename := fmt.Sprintf("%d_%s", timestamp, safeName)
	fullPath := filepath.Join(finalDir, filename)

	// Verify the resolved path is inside finalDir even after sanitisation.
	// This is a defence-in-depth check — sanitiseFilename should prevent this,
	// but we never rely on a single layer for path safety.
	cleanFull := filepath.Clean(fullPath)
	cleanDir := filepath.Clean(finalDir)
	if !strings.HasPrefix(cleanFull, cleanDir+string(filepath.Separator)) {
		return UploadedFile{}, fmt.Errorf("upload: resolved path escapes upload directory")
	}

	// Save
	dst, err := os.Create(fullPath)
	if err != nil {
		return UploadedFile{}, err
	}
	defer func() {
		dst.Close()
		if err != nil {
			os.Remove(fullPath) // clean up on any error path
		}
	}()

	limitedFile := io.LimitReader(file, maxBytes+1)
	written, copyErr := io.Copy(dst, limitedFile)
	if int64(written) > maxBytes {
		_ = os.Remove(fullPath)
		return UploadedFile{}, fmt.Errorf("file exceeds %d MB limit", limitMB)
	}
	if copyErr != nil {
		return UploadedFile{}, copyErr
	}

	// Return relative path for storage in DB
	// If StorePath was "avatars", result is "avatars/123_me.jpg"
	relativePath := filepath.Join(config.StorePath, filename)

	// Ensure we use forward slashes for web paths
	relativePath = filepath.ToSlash(relativePath)

	return UploadedFile{
		Name:     header.Filename,
		Path:     relativePath,
		Size:     header.Size,
		Ext:      extFromMIME(sniffed, safeName),
		MimeType: sniffed,
	}, nil
}

// DeleteFile deletes a file from the storage.
// basePath: absolute path to storage root.
// relativePath: path stored in DB (e.g. "avatars/123.jpg").
func DeleteFile(basePath, relativePath string) error {
	cleanBase := filepath.Clean(basePath)
	fullPath := filepath.Join(cleanBase, relativePath)
	cleanFull := filepath.Clean(fullPath)

	// Ensure the resolved path is actually inside basePath
	if !strings.HasPrefix(cleanFull, cleanBase+string(filepath.Separator)) {
		return fmt.Errorf("invalid file path: directory traversal detected")
	}

	if _, err := os.Stat(cleanFull); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", cleanFull)
	}

	return os.Remove(cleanFull)
}

// GetParamInt reads a URL path parameter by name and returns it as int.
// Returns 0 if the parameter is missing or cannot be parsed as an integer.
func GetParamInt(ctx *gin.Context, key string) int {
	val := ctx.Param(key)
	if val == "" {
		return 0
	}
	i, _ := strconv.Atoi(val)
	return i
}

// GetQueryInt reads a URL query parameter by name and returns it as int.
// Returns 0 if the parameter is missing or cannot be parsed as an integer.
func GetQueryInt(ctx *gin.Context, key string) int {
	val := ctx.Query(key)
	if val == "" {
		return 0
	}
	i, _ := strconv.Atoi(val)
	return i
}

// GetPathID reads the :id URL path parameter, validates it is a positive integer,
// and writes a 400 Bad Request using the standard error envelope if not.
// Returns (id, true) on success, (0, false) after writing the error response.
// The handler must return immediately on false.
func GetPathID(ctx *gin.Context) (int, bool) {
	id := GetParamInt(ctx, "id")
	if id <= 0 {
		_ = ctx.Error(apperr.BadRequest("invalid id: must be a positive integer"))
		ctx.Abort()
		return 0, false
	}
	return id, true
}
