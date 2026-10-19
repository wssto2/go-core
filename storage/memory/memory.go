package memory

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wssto2/go-core/apperr"
)

// fileMode is the mode every object reports.
const fileMode = 0o644

// maxSize is the hard cap on one object.
const maxSize = 100 * 1024 * 1024 // 100 MiB

type entry struct {
	data []byte
	mod  time.Time
}

// Driver keeps objects in memory, for tests. It has the capabilities of the local driver: Open for seeking, the same
// Put rules and an idempotent Delete.
type Driver struct {
	data map[string]entry
	mu   sync.RWMutex
}

func New() (*Driver, error) {
	return &Driver{
		data: make(map[string]entry),
	}, nil
}

func (d *Driver) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	f, _, err := d.Open(ctx, key)
	if err != nil {
		return nil, err
	}

	return f, nil
}

// Open opens the object for reading and seeking, with its file info.
func (d *Driver) Open(_ context.Context, key string) (io.ReadSeekCloser, fs.FileInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	e, ok := d.data[key]
	if !ok {
		return nil, nil, apperr.NotFound("key not found")
	}

	return nopCloser{bytes.NewReader(e.data)}, info{name: path.Base(key), size: int64(len(e.data)), mod: e.mod}, nil
}

// Put stores the whole stream at key, replacing what is there. size is the length the stream must have: 0 (or less)
// means unknown; any other value makes a longer or shorter stream an error.
func (d *Driver) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	_, err := d.put(key, r, size, true)

	return err
}

// PutNew stores the stream at key only when nothing is there, under the driver's lock, and reports whether it did.
func (d *Driver) PutNew(_ context.Context, key string, r io.Reader, size int64, _ string) (bool, error) {
	return d.put(key, r, size, false)
}

// Delete removes the object. A missing object is not an error.
func (d *Driver) Delete(ctx context.Context, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.data, key)
	return nil
}

func (d *Driver) URL(ctx context.Context, key string) (string, error) {
	return "", nil
}

// List returns the keys under the directory prefix ("" for all), sorted.
func (d *Driver) List(ctx context.Context, prefix string) ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	prefix = strings.TrimSuffix(prefix, "/")

	var keys []string
	for key := range d.data {
		if prefix == "" || key == prefix || strings.HasPrefix(key, prefix+"/") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (d *Driver) Exists(ctx context.Context, key string) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	_, ok := d.data[key]
	return ok, nil
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

// info is the fs.FileInfo of an object.
type info struct {
	name string
	size int64
	mod  time.Time
}

func (i info) Name() string       { return i.name }
func (i info) Size() int64        { return i.size }
func (i info) Mode() fs.FileMode  { return fileMode }
func (i info) ModTime() time.Time { return i.mod }
func (i info) IsDir() bool        { return false }
func (i info) Sys() any           { return nil }

func (d *Driver) put(key string, r io.Reader, size int64, replace bool) (bool, error) {
	if size > maxSize {
		return false, apperr.BadRequest(fmt.Sprintf("storage: size %d exceeds maximum allowed (%d bytes)", size, maxSize))
	}

	data, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err != nil {
		return false, apperr.Internal(err)
	}

	if len(data) > maxSize {
		return false, apperr.BadRequest(fmt.Sprintf("storage: the stream exceeds the maximum allowed (%d bytes)", maxSize))
	}

	if size > 0 && int64(len(data)) != size {
		return false, apperr.BadRequest(fmt.Sprintf("storage: the stream is not the declared %d bytes", size))
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, taken := d.data[key]; taken && !replace {
		return false, nil
	}

	d.data[key] = entry{data: data, mod: time.Now()}

	return true, nil
}
