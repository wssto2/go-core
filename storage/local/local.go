package local

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/wssto2/go-core/apperr"
)

// LocalDriver stores objects on the local filesystem under Root.
type Driver struct {
	Root string
}

// New constructs a Driver ensuring the root directory exists.
func New(root string) (*Driver, error) {
	if root == "" {
		return nil, apperr.BadRequest("storage root path is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return nil, apperr.Internal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return &Driver{Root: resolvedRoot}, nil
}

// safePath validates key and returns the absolute path within d.Root.
// It rejects empty keys and keys that escape the root directory.
func (d *Driver) safePath(key string) (string, error) {
	if key == "" {
		return "", apperr.BadRequest("storage key must not be empty")
	}
	root := filepath.Clean(d.Root)
	p := filepath.Join(root, key)
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", apperr.Internal(err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", apperr.BadRequest("storage key escapes root directory")
	}
	if err := ensureSymlinkFreeParents(root, p); err != nil {
		return "", err
	}
	return p, nil
}

func ensureSymlinkFreeParents(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return apperr.Internal(err)
	}
	if rel == "." {
		return nil
	}
	current := root
	parts := strings.Split(rel, string(filepath.Separator))
	for i := 0; i < len(parts)-1; i++ {
		current = filepath.Join(current, parts[i])
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return apperr.Internal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return apperr.BadRequest("storage path traverses a symlinked directory")
		}
		if !info.IsDir() {
			return apperr.BadRequest("storage path has a non-directory parent")
		}
	}
	return nil
}

// Put writes the provided reader to disk at key.
func (d *Driver) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	p, err := d.safePath(key)
	if err != nil {
		return err
	}
	// Reject symlinks to prevent write-through attacks on pre-planted symlinks.
	if info, lerr := os.Lstat(p); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return apperr.BadRequest("storage key refers to a symlink")
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return apperr.Internal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		return apperr.Internal(err)
	}
	defer func() { _ = f.Close() }()

	// Limit reader to declared size to prevent unbounded disk writes.
	var reader io.Reader = r
	if size > 0 {
		reader = io.LimitReader(r, size)
	}
	if _, err := io.Copy(f, reader); err != nil {
		_ = os.Remove(p) // clean up partial file on write error
		return apperr.Internal(err)
	}
	// Flush to underlying storage before returning to ensure durability.
	if err := f.Sync(); err != nil {
		_ = os.Remove(p)
		return apperr.Internal(err)
	}
	return nil
}

// Get opens the object for reading.
func (d *Driver) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := d.safePath(key)
	if err != nil {
		return nil, err
	}
	// Reject symlinks to prevent traversal to files outside the storage root.
	if info, lerr := os.Lstat(p); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, apperr.BadRequest("storage key refers to a symlink")
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apperr.NotFound(fmt.Sprintf("object %s not found", key))
		}
		return nil, apperr.Internal(err)
	}
	return f, nil
}

// Delete removes the object from disk.
func (d *Driver) Delete(ctx context.Context, key string) error {
	p, err := d.safePath(key)
	if err != nil {
		return err
	}
	// Reject symlinks to prevent deletion of files outside the storage root.
	if info, lerr := os.Lstat(p); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return apperr.BadRequest("storage key refers to a symlink")
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return apperr.NotFound(fmt.Sprintf("object %s not found", key))
		}
		return apperr.Internal(err)
	}
	return nil
}

// URL returns a file:// URL to the object on disk.
func (d *Driver) URL(ctx context.Context, key string) (string, error) {
	p, err := d.safePath(key)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", apperr.Internal(err)
	}
	u := url.URL{Scheme: "file", Path: abs}
	return u.String(), nil
}

// List returns all keys with the given prefix.
func (d *Driver) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	rootPrefix, err := d.safePath(prefix)
	if err != nil {
		return nil, err
	}
	err = filepath.Walk(rootPrefix, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(d.Root, path)
		if err != nil {
			return err
		}
		keys = append(keys, rel)
		return nil
	})
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return keys, nil
}

// Exists checks if the key exists.
func (d *Driver) Exists(ctx context.Context, key string) (bool, error) {
	p, err := d.safePath(key)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(p)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return false, apperr.BadRequest("storage key refers to a symlink")
		}
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, apperr.Internal(err)
}
