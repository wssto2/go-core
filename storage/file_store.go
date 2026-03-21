package storage

import "context"

type FileStore interface {
	Exists(ctx context.Context, key string) (bool, error)
	Save(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}
