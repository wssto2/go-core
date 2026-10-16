package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/storage"
)

type host struct {
	reg   *storage.Registry
	fails []string
}

func (disks *host) Disk(name string) (*storage.Files, bool) { return disks.reg.Disk(name) }
func (disks *host) Fail(what, _ string)                     { disks.fails = append(disks.fails, what) }

func newHost(t *testing.T, specs ...storage.DiskSpec) *host {
	t.Helper()

	disks := &host{reg: storage.NewRegistry(), fails: nil}
	for _, s := range specs {
		require.NoError(t, disks.reg.Add(s))
	}

	return disks
}

func put(t *testing.T, files *storage.Files, key, body string) {
	t.Helper()
	require.NoError(t, files.Put(context.Background(), key, bytes.NewBufferString(body), 0, ""))
}

func TestRegistryRefusesWhatCannotWork(t *testing.T) {
	t.Parallel()

	reg := storage.NewRegistry()
	require.NoError(t, reg.Add(storage.Disk("a").Memory()))

	file := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	for name, spec := range map[string]storage.DiskSpec{
		"duplicate":     storage.Disk("a").Memory(),
		"empty name":    storage.Disk("").Memory(),
		"no driver":     storage.Disk("b"),
		"two drivers":   storage.Disk("c").Memory().Memory(),
		"bad root":      storage.Disk("d").Local(""),
		"bad public":    storage.Disk("e").Memory().Public("media"),
		"empty public":  storage.Disk("f").Memory().Public(""),
		"nil driver":    storage.Disk("g").Driver(nil),
		"spaced name":   storage.Disk(" h").Memory(),
		"file as root":  storage.Disk("i").Local(filepath.Join(file, "sub")),
		"public + dups": storage.Disk("a").Memory().Public("/m"),
	} {
		assert.Error(t, reg.Add(spec), name)
	}
}

func TestUseOfAnUnknownDiskIsAStartupProblemNotAFirstUseSurprise(t *testing.T) {
	t.Parallel()

	disks := newHost(t, storage.Disk("uploads").Memory())

	assert.Empty(t, disks.fails)
	storage.Use(disks, "uploads")
	assert.Empty(t, disks.fails)

	ghost := storage.Use(disks, "uploadz")
	require.Len(t, disks.fails, 1)
	assert.Contains(t, disks.fails[0], `"uploadz"`)

	_, err := ghost.Exists(context.Background(), "x")
	require.Error(t, err)
	assert.Error(t, ghost.Folder("a").Put(context.Background(), "x", bytes.NewReader(nil), 0, ""))
}

func TestPrivateDiskHasNoURLAndPublicOneHasEscapedURLs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	disks := newHost(t,
		storage.Disk("uploads").Memory(),
		storage.Disk("public").Memory().Public("/media/"),
		storage.Disk("cdn").Memory().Public("https://cdn.example.com/m"),
	)

	_, err := storage.Use(disks, "uploads").URL(ctx, "a.pdf")
	require.Error(t, err)
	assert.False(t, storage.Use(disks, "uploads").IsPublic())

	photos := storage.Use(disks, "public").Folder("vehicles")
	link, err := photos.URL(ctx, "123/photos/front view.jpg")
	require.NoError(t, err)
	assert.Equal(t, "/media/vehicles/123/photos/front%20view.jpg", link)
	assert.True(t, photos.IsPublic())

	link, err = storage.Use(disks, "cdn").URL(ctx, "a.jpg")
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/m/a.jpg", link)

	_, err = photos.URL(ctx, "../x")
	assert.Error(t, err, "the URL of a key that escapes is refused too")
}

func TestFolderScopesTheDisk(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	disks := newHost(t, storage.Disk("d").Memory())
	disk := storage.Use(disks, "d")

	put(t, disk, "docs/a.txt", "a")
	put(t, disk, "docs-old/b.txt", "b")
	put(t, disk, "docs/2024/c.txt", "c")
	put(t, disk, "root.txt", "r")

	docs := disk.Folder("docs")
	keys, err := docs.List(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"2024/c.txt", "a.txt"}, keys, "a sibling folder with the same name prefix is not listed")

	nested := docs.Folder("2024")
	keys, err = nested.List(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"c.txt"}, keys)

	found, err := docs.Exists(ctx, "a.txt")
	require.NoError(t, err)
	assert.True(t, found)

	found, err = docs.Exists(ctx, "root.txt")
	require.NoError(t, err)
	assert.False(t, found, "root.txt is outside the folder")

	escapes := []string{"../root.txt", "/root.txt", "a/../../root.txt", "..", "a/./b", "a//b", "a\\b", "", "a/", "x\x00y"}

	for _, bad := range escapes {
		_, err := docs.Get(ctx, bad)
		require.Error(t, err, "Get %q", bad)
		require.Error(t, docs.Put(ctx, bad, bytes.NewReader(nil), 0, ""), "Put %q", bad)
		require.Error(t, docs.Delete(ctx, bad), "Delete %q", bad)

		_, err = docs.Exists(ctx, bad)
		require.Error(t, err, "Exists %q", bad)
	}

	_, err = docs.List(ctx, "../")
	require.Error(t, err)

	for _, bad := range []string{"..", "/etc", "a/../..", ""} {
		_, err := disk.Folder(bad).Exists(ctx, "x")
		require.Error(t, err, "Folder %q", bad)
	}

	require.NoError(t, docs.Delete(ctx, "a.txt"))
	found, err = disk.Exists(ctx, "root.txt")
	require.NoError(t, err)
	assert.True(t, found)
}

func TestDeleteOfAMissingFileIsNotAnError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	disks := newHost(t, storage.Disk("m").Memory(), storage.Disk("l").Local(t.TempDir()))

	for _, name := range []string{"m", "l"} {
		files := storage.Use(disks, name).Folder("x")
		put(t, files, "a", "1")
		assert.NoError(t, files.Delete(ctx, "a"), name)
		assert.NoError(t, files.Delete(ctx, "a"), name)
		assert.NoError(t, files.Delete(ctx, "never/was"), name)
	}
}

func TestPutWritesTheWholeStreamAndReplaces(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	disks := newHost(t, storage.Disk("m").Memory(), storage.Disk("l").Local(t.TempDir()))

	for _, name := range []string{"m", "l"} {
		files := storage.Use(disks, name)
		body := bytes.Repeat([]byte("x"), 5000)

		require.NoError(t, files.Put(ctx, "a/b.bin", bytes.NewReader(body), 0, ""), name)
		require.NoError(t, files.Put(ctx, "a/exact.bin", bytes.NewReader(body), 5000, ""), name)
		assert.Equal(t, body, read(t, files, "a/b.bin"), "size 0 is unknown: everything is written (%s)", name)

		longer := files.Put(ctx, "a/long.bin", bytes.NewReader(body), 10, "")
		require.Error(t, longer, "a longer stream is refused, not cut (%s)", name)
		require.Error(t, files.Put(ctx, "a/short.bin", bytes.NewReader(body), 9000, ""), "a shorter one too (%s)", name)
		found, _ := files.Exists(ctx, "a/long.bin")
		assert.False(t, found, name)

		require.NoError(t, files.Put(ctx, "a/b.bin", bytes.NewBufferString("new"), 3, ""), name)
		assert.Equal(t, []byte("new"), read(t, files, "a/b.bin"), "an existing key is overwritten (%s)", name)
	}
}

func read(t *testing.T, files storage.Driver, key string) []byte {
	t.Helper()

	reg, err := files.Get(context.Background(), key)
	require.NoError(t, err)

	defer func() { _ = reg.Close() }()

	b, err := io.ReadAll(reg)
	require.NoError(t, err)

	return b
}

func TestAFailedPutOnLocalLeavesNoPartialFileAndKeepsTheOldOne(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	disks := newHost(t, storage.Disk("l").Local(root))
	files := storage.Use(disks, "l")

	put(t, files, "d/a.txt", "old")

	broken := io.MultiReader(bytes.NewBufferString("partial"), failingReader{})
	require.Error(t, files.Put(ctx, "d/a.txt", broken, 0, ""))
	require.Error(t, files.Put(ctx, "d/new.txt", io.MultiReader(bytes.NewBufferString("partial"), failingReader{}), 0, ""))

	assert.Equal(t, []byte("old"), read(t, files, "d/a.txt"))

	entries, err := os.ReadDir(filepath.Join(root, "d"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary or partial file is left")
	assert.Equal(t, "a.txt", entries[0].Name())

	keys, err := files.List(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"d/a.txt"}, keys)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestLocalDiskRefusesASymlinkedFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link.txt")))

	files := storage.Use(newHost(t, storage.Disk("l").Local(root)), "l")

	_, err := files.Get(ctx, "link.txt")
	require.True(t, apperr.HasCode(err, apperr.CodeBadRequest))

	_, _, err = files.Open(ctx, "link.txt")
	require.True(t, apperr.HasCode(err, apperr.CodeBadRequest))
	require.Error(t, files.Delete(ctx, "link.txt"))
	require.Error(t, files.Put(ctx, "link.txt", bytes.NewBufferString("x"), 0, ""))

	kept, err := os.ReadFile(outside) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, "secret", string(kept), "nothing was written through the link")

	_, err = files.Exists(ctx, "link.txt")
	require.Error(t, err)
}
