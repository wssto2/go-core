package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/wssto2/go-core/apperr"
)

// Files is a disk, or a folder of one: the handle features keep. It is a Driver, so anything written against a Driver
// takes it, and it adds what a Driver cannot promise: keys are relative to the folder, a key that leaves it is
// refused, and URL follows the disk's kind.
type Files struct {
	driver Driver
	disk   string
	// prefix is the folder inside the disk, "" for the whole disk; keys never contain a leading or trailing slash.
	prefix string
	base   string
	public bool
	// err is why every call fails: the disk is unknown, or the folder is not a valid one.
	err error
}

var errPrivate = errors.New("it is private: its files are served through the application and have no URL")

var (
	_ Driver  = (*Files)(nil)
	_ Opener  = (*Files)(nil)
	_ Creator = (*Files)(nil)
)

// Folder returns the files of this disk or folder under prefix, "vehicle-documents" or "vehicles/2024". Nothing
// outside it can be read, listed or deleted through the result, and folders nest. A prefix that is empty, absolute or
// climbs with ".." gives a folder whose every call fails with that reason.
func (f *Files) Folder(prefix string) *Files {
	if f.err != nil {
		return f
	}

	scoped := *f

	err := checkKey(prefix)
	if err != nil {
		scoped.err = err

		return &scoped
	}

	scoped.prefix = join(f.prefix, prefix)

	return &scoped
}

// IsPublic reports whether the files are reachable by URL.
func (f *Files) IsPublic() bool { return f.public }

// Put writes the whole stream at key, replacing what is there; a failed write leaves no partial file. size is the
// length the stream must have, 0 when unknown.
func (f *Files) Put(ctx context.Context, key string, body io.Reader, size int64, mime string) error {
	full, err := f.resolve(key)
	if err != nil {
		return err
	}

	return f.driver.Put(ctx, full, body, size, mime) //nolint:wrapcheck // a driver's own apperr error
}

// PutNew stores the stream at key only when nothing is there, atomically, and reports whether it did. A driver that
// cannot do that atomically returns errors.ErrUnsupported (see Creator).
func (f *Files) PutNew(ctx context.Context, key string, body io.Reader, size int64, mime string) (bool, error) {
	full, err := f.resolve(key)
	if err != nil {
		return false, err
	}

	c, ok := f.driver.(Creator)
	if !ok {
		return false, errors.ErrUnsupported
	}

	return c.PutNew(ctx, full, body, size, mime) //nolint:wrapcheck // a driver's own apperr error
}

var errNoFreeName = errors.New("storage: no free name")

// maxUniqueTries bounds the numbered names PutUnique tries before it gives up.
const maxUniqueTries = 10000

// PutUnique stores the stream under name, or under "base-1.ext", "base-2.ext", … when that is taken, and returns the
// key it used, relative to the folder. Two callers saving the same name get different keys and both bodies survive:
// the choice and the write are one atomic step in the driver (see Creator), where an Exists-then-Put loop races. The
// body is read once, so a name that is taken costs a write for each try on a driver that has to consume the stream
// first; pass a name through SafeName when it comes from a client. A driver without Creator returns
// errors.ErrUnsupported.
func (f *Files) PutUnique(ctx context.Context, name string, body io.Reader, size int64, mime string) (string, error) {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)

	// The body can be offered to only one driver call, so it is buffered once for the retries.
	seeker, ok := body.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(body)
		if err != nil {
			return "", apperr.Internal(err)
		}

		seeker = bytes.NewReader(data)
	}

	key := name

	for try := 1; try <= maxUniqueTries; try++ {
		created, err := f.PutNew(ctx, key, seeker, size, mime)
		if err != nil {
			return "", err
		}

		if created {
			return key, nil
		}

		_, err = seeker.Seek(0, io.SeekStart)
		if err != nil {
			return "", apperr.Internal(err)
		}

		key = fmt.Sprintf("%s-%d%s", base, try, ext)
	}

	return "", apperr.Internal(fmt.Errorf("%w for %q after %d tries", errNoFreeName, name, maxUniqueTries))
}

// SafeName reduces a client's file name to a safe bare key: the directory part dropped (either slash), lower-cased,
// every run of characters that are not letters, digits or dots folded to one "-", and "-" and "." trimmed from the
// ends. A name with nothing left becomes "document".
func SafeName(name string) string {
	name = strings.ToLower(path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/")))

	var out strings.Builder

	lastDash := false

	for _, r := range name {
		switch {
		case r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r):
			out.WriteRune(r)

			lastDash = false
		case !lastDash:
			out.WriteRune('-')

			lastDash = true
		}
	}

	clean := strings.Trim(out.String(), "-.")
	if clean == "" {
		return "document"
	}

	return clean
}

// Get opens the file for reading. A missing file is apperr.NotFound.
func (f *Files) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	full, err := f.resolve(key)
	if err != nil {
		return nil, err
	}

	return f.driver.Get(ctx, full) //nolint:wrapcheck // a driver's own apperr error
}

// Open opens the file for reading and seeking. A driver that cannot returns errors.ErrUnsupported.
func (f *Files) Open(ctx context.Context, key string) (io.ReadSeekCloser, fs.FileInfo, error) {
	full, err := f.resolve(key)
	if err != nil {
		return nil, nil, err
	}

	o, ok := f.driver.(Opener)
	if !ok {
		return nil, nil, errors.ErrUnsupported
	}

	return o.Open(ctx, full) //nolint:wrapcheck // a driver's own apperr error
}

// Delete removes the file. A missing file is not an error.
func (f *Files) Delete(ctx context.Context, key string) error {
	full, err := f.resolve(key)
	if err != nil {
		return err
	}

	err = f.driver.Delete(ctx, full)
	if err != nil && !apperr.HasCode(err, apperr.CodeNotFound) {
		return err //nolint:wrapcheck // a driver's own apperr error
	}

	return nil
}

// Exists reports whether the file is there.
func (f *Files) Exists(ctx context.Context, key string) (bool, error) {
	full, err := f.resolve(key)
	if err != nil {
		return false, err
	}

	return f.driver.Exists(ctx, full) //nolint:wrapcheck // a driver's own apperr error
}

// URL is where anyone can open the file, on a public disk: "/media/vehicles/123/front.jpg". A private disk has no URL,
// its files are served through the application (see web.Serve), so asking is an error.
func (f *Files) URL(_ context.Context, key string) (string, error) {
	full, err := f.resolve(key)
	if err != nil {
		return "", err
	}

	if !f.public {
		return "", apperr.Internal(fmt.Errorf("storage: disk %s: %w", f.disk, errPrivate))
	}

	segments := strings.Split(full, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}

	return f.base + "/" + strings.Join(segments, "/"), nil
}

// List returns the keys under the directory prefix of this folder, "" for all of them, relative to the folder.
func (f *Files) List(ctx context.Context, prefix string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}

	dir := f.prefix
	if prefix = strings.TrimSuffix(prefix, "/"); prefix != "" {
		full, err := f.resolve(prefix)
		if err != nil {
			return nil, err
		}

		dir = full
	}

	keys, err := f.driver.List(ctx, dir)
	if err != nil {
		return nil, err //nolint:wrapcheck // a driver's own apperr error
	}

	out := make([]string, 0, len(keys))

	for _, k := range keys {
		// Defence in depth: a driver that matches by string prefix must not leak a sibling folder ("vehicles-old/").
		if f.prefix == "" {
			out = append(out, k)
		} else if rest, ok := strings.CutPrefix(k, f.prefix+"/"); ok {
			out = append(out, rest)
		}
	}

	return out, nil
}

// resolve checks key and returns it inside the disk.
func (f *Files) resolve(key string) (string, error) {
	if f.err != nil {
		return "", f.err
	}

	err := checkKey(key)
	if err != nil {
		return "", err
	}

	return join(f.prefix, key), nil
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}

	return prefix + "/" + key
}

// checkKey refuses a key that is empty, absolute, holds a backslash or a NUL, or has an empty, "." or ".." segment.
func checkKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.ContainsAny(key, "\\\x00") || path.Clean(key) != key {
		return apperr.BadRequest("storage key must be a relative path inside the folder")
	}

	if slices.Contains(strings.Split(key, "/"), "..") {
		return apperr.BadRequest("storage key escapes the folder")
	}

	return nil
}
