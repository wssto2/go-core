package local

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

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
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, apperr.Internal(err)
	}
	return &Driver{Root: root}, nil
}

// Put writes the provided reader to disk at key.
func (d *Driver) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	p := filepath.Join(d.Root, key)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return apperr.Internal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		return apperr.Internal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, r); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// Get opens the object for reading.
func (d *Driver) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	p := filepath.Join(d.Root, key)
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
	p := filepath.Join(d.Root, key)
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
	p := filepath.Join(d.Root, key)
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
	rootPrefix := filepath.Join(d.Root, prefix)
	err := filepath.Walk(rootPrefix, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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
	p := filepath.Join(d.Root, key)
	_, err := os.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, apperr.Internal(err)
}
