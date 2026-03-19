package web

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/utils"
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

// UploadFile handles a file upload from a Gin context.
// Form field name must be "photo" (as per original code, maybe make this configurable?).
// Let's make the form key configurable or default to "file".
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
			allowedMime = []string{"image/jpeg", "image/png", "image/gif", "image/webp", "image/svg+xml", "image/bmp"}
		} else {
			// Default broader list
			allowedMime = []string{
				"image/jpeg", "image/png", "image/gif", "image/webp", "image/svg+xml", "image/bmp",
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

	contentType := header.Header.Get("Content-Type")
	// If content-type is empty, we might want to sniff it, but for now rely on header
	if !utils.StringInSlice(contentType, allowedMime) {
		return UploadedFile{}, fmt.Errorf("file type '%s' is not allowed", contentType)
	}

	// Prepare directory
	finalDir := filepath.Join(config.BaseDir, config.StorePath)
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		return UploadedFile{}, fmt.Errorf("failed to create upload directory: %w", err)
	}

	// Generate filename: timestamp_safe-original-name
	timestamp := time.Now().Unix()
	safeName := strings.ReplaceAll(header.Filename, " ", "_")
	filename := fmt.Sprintf("%d_%s", timestamp, safeName)
	fullPath := filepath.Join(finalDir, filename)

	// Save
	dst, err := os.Create(fullPath)
	if err != nil {
		return UploadedFile{}, err
	}
	defer dst.Close()

	if _, err = io.Copy(dst, file); err != nil {
		return UploadedFile{}, err
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
		Ext:      filepath.Ext(header.Filename),
		MimeType: contentType,
	}, nil
}

// DeleteFile deletes a file from the storage.
// basePath: absolute path to storage root.
// relativePath: path stored in DB (e.g. "avatars/123.jpg").
func DeleteFile(basePath, relativePath string) error {
	// Security check: prevent directory traversal
	cleanRel := filepath.Clean(relativePath)
	if strings.Contains(cleanRel, "..") {
		return fmt.Errorf("invalid file path: traversal detected")
	}

	fullPath := filepath.Join(basePath, cleanRel)
	
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", fullPath)
	}

	return os.Remove(fullPath)
}

// Helper to get int param from context
func GetParamInt(ctx *gin.Context, key string) int {
	val := ctx.Param(key)
	if val == "" {
		return 0
	}
	i, _ := strconv.Atoi(val)
	return i
}

// Helper to get int query from context
func GetQueryInt(ctx *gin.Context, key string) int {
	val := ctx.Query(key)
	if val == "" {
		return 0
	}
	i, _ := strconv.Atoi(val)
	return i
}
