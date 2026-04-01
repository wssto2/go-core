package memory

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/wssto2/go-core/apperr"
)

type Driver struct {
	data map[string][]byte
	mu   sync.RWMutex
}

func New() (*Driver, error) {
	return &Driver{
		data: make(map[string][]byte),
	}, nil
}

func (d *Driver) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	data, ok := d.data[key]
	if !ok {
		return nil, apperr.NotFound("key not found")
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

func (d *Driver) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	data := make([]byte, size)
	_, err := io.ReadFull(r, data)
	if err != nil {
		return apperr.Internal(err)
	}

	d.data[key] = data
	return nil
}

func (d *Driver) Delete(ctx context.Context, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.data, key)
	return nil
}

func (d *Driver) URL(ctx context.Context, key string) (string, error) {
	return "", nil
}

func (d *Driver) List(ctx context.Context, prefix string) ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var keys []string
	for key := range d.data {
		if key == prefix || key > prefix {
			break
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (d *Driver) Exists(ctx context.Context, key string) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	_, ok := d.data[key]
	return ok, nil
}
