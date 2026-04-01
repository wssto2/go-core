package storage

import (
	"context"
	"io"
)

// Driver is the storage abstraction for file-like objects.
// Minimal set of operations needed by consumers.
type Driver interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	URL(ctx context.Context, key string) (string, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Exists(ctx context.Context, key string) (bool, error)
}
