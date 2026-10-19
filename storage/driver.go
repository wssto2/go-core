package storage

import (
	"context"
	"io"
	"io/fs"
)

// Driver is the storage abstraction for file-like objects.
// Minimal set of operations needed by consumers.
//
// Put writes the whole stream at key, replacing what is there, and leaves nothing behind when it fails. size is the
// length the stream must have; 0 means unknown. Delete of a missing key is not an error. List returns the keys under
// the directory prefix, "" for all of them.
type Driver interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	URL(ctx context.Context, key string) (string, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// Opener is the capability of a driver (the local and memory ones have it) to open an object for seeking, which is
// what serving a byte range or answering If-Modified-Since needs. A caller asserts it and falls back to Get.
type Opener interface {
	Open(ctx context.Context, key string) (io.ReadSeekCloser, fs.FileInfo, error)
}

// Creator is the capability of a driver (the local and memory ones have it) to store an object only when nothing is at
// the key, in one atomic step: PutNew reports false, and writes nothing, when the key is taken, and never replaces an
// object. It is what Files.PutUnique is built on; a driver that cannot promise the atomicity must not implement it,
// because a check-then-write fallback loses a file when two callers pick the same name.
type Creator interface {
	PutNew(ctx context.Context, key string, r io.Reader, size int64, mime string) (created bool, err error)
}
