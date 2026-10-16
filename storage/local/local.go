package local

import (
	"context"
	"fmt"
	"io"
	"io/fs"
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

// tempPrefix names the file a Put writes before it renames it into place; List never reports one.
const tempPrefix = ".gocore-put-"

// fileMode is the permission of a stored file, before the umask.
const (
	fileMode = 0o644
	dirMode  = 0o755
)

// Put writes r to key, replacing what is there. The bytes go to a temporary file in the same directory and are renamed
// into place, so a failed write leaves neither a partial file nor a damaged earlier one. size is the length the stream
// must have: 0 (or less) means unknown, and any other value makes a longer or shorter stream an error rather than a
// silently truncated file.
func (d *Driver) Put(_ context.Context, key string, body io.Reader, size int64, _ string) error {
	abs, err := d.safePath(key)
	if err != nil {
		return err
	}

	// Reject symlinks to prevent write-through attacks on pre-planted symlinks.
	err = refuseSymlink(abs)
	if err != nil {
		return err
	}

	dir := filepath.Dir(abs)

	err = os.MkdirAll(dir, dirMode)
	if err != nil {
		return apperr.Internal(err)
	}

	tmp, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return apperr.Internal(err)
	}

	renamed := false

	defer func() {
		if !renamed {
			_ = tmp.Close()           // best-effort: it is already closed once the write finished
			_ = os.Remove(tmp.Name()) // best-effort cleanup of partial file
		}
	}()

	err = writeAll(tmp, body, size)
	if err != nil {
		return err
	}

	err = os.Rename(tmp.Name(), abs)
	if err != nil {
		return apperr.Internal(err)
	}

	renamed = true

	return nil
}

// writeAll copies the stream into f, checks its length against size when that is known, and closes f.
func writeAll(tmp *os.File, body io.Reader, size int64) error {
	// One byte more than declared is read so a longer stream is seen, and refused, instead of cut off.
	reader := body
	if size > 0 {
		reader = io.LimitReader(body, size+1)
	}

	n, err := io.Copy(tmp, reader)
	if err != nil {
		return apperr.Internal(err)
	}

	if size > 0 && n != size {
		return apperr.BadRequest(fmt.Sprintf("storage: the stream is not the declared %d bytes", size))
	}

	// Flush to underlying storage before returning to ensure durability.
	err = tmp.Sync()
	if err != nil {
		return apperr.Internal(err)
	}

	err = tmp.Chmod(fileMode)
	if err != nil {
		return apperr.Internal(err)
	}

	err = tmp.Close()
	if err != nil {
		return apperr.Internal(err)
	}

	return nil
}

// refuseSymlink fails when the file itself is a symlink.
func refuseSymlink(abs string) error {
	info, err := os.Lstat(abs)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return apperr.BadRequest("storage key refers to a symlink")
	}

	return nil
}

// Get opens the object for reading.
func (d *Driver) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	f, _, err := d.Open(ctx, key)
	if err != nil {
		return nil, err
	}

	return f, nil
}

// Open opens the object for reading and seeking, with its file info, for a caller that serves ranges.
func (d *Driver) Open(_ context.Context, key string) (io.ReadSeekCloser, fs.FileInfo, error) {
	abs, err := d.safePath(key)
	if err != nil {
		return nil, nil, err
	}

	// Reject symlinks to prevent traversal to files outside the storage root.
	err = refuseSymlink(abs)
	if err != nil {
		return nil, nil, err
	}

	file, err := os.Open(abs) //nolint:gosec // abs is safePath's: inside the root, no symlinks
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, apperr.NotFound(fmt.Sprintf("object %s not found", key))
		}

		return nil, nil, apperr.Internal(err)
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close() // best-effort: the Stat failure is what is returned

		return nil, nil, apperr.Internal(err)
	}

	if info.IsDir() {
		_ = file.Close() // best-effort: a directory is not an object

		return nil, nil, apperr.NotFound(fmt.Sprintf("object %s not found", key))
	}

	return file, info, nil
}

// Delete removes the object from disk. A missing object is not an error.
func (d *Driver) Delete(_ context.Context, key string) error {
	abs, err := d.safePath(key)
	if err != nil {
		return err
	}

	// Reject symlinks to prevent deletion of files outside the storage root.
	err = refuseSymlink(abs)
	if err != nil {
		return err
	}

	err = os.Remove(abs)
	if err != nil && !os.IsNotExist(err) {
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

// List returns the keys under the directory prefix, "" for all of them, with forward slashes.
func (d *Driver) List(_ context.Context, prefix string) ([]string, error) {
	start := d.Root // an empty prefix lists everything

	if prefix != "" {
		abs, err := d.safePath(prefix)
		if err != nil {
			return nil, err
		}

		start = abs
	}

	keys := []string{}

	err := filepath.Walk(start, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() || strings.HasPrefix(info.Name(), tempPrefix) {
			return nil
		}

		rel, err := filepath.Rel(d.Root, path)
		if err != nil {
			return err
		}

		keys = append(keys, filepath.ToSlash(rel))

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
