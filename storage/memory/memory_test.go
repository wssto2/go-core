package memory

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryDriver_List_ReturnsPrefixedKeys(t *testing.T) {
	d, err := New()
	require.NoError(t, err)

	ctx := context.Background()
	for _, key := range []string{"a/1", "a/2", "b/1"} {
		data := []byte("data")
		require.NoError(t, d.Put(ctx, key, bytes.NewReader(data), int64(len(data)), ""))
	}

	// Repeated calls must return the same result (map iteration is non-deterministic).
	for i := 0; i < 10; i++ {
		keys, err := d.List(ctx, "a/")
		require.NoError(t, err)
		assert.Equal(t, []string{"a/1", "a/2"}, keys,
			"List(\"a/\") must return exactly [a/1, a/2] in sorted order")
	}

	// Prefix that matches nothing.
	keys, err := d.List(ctx, "c/")
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestMemoryDriver_UnknownSizeWritesEverythingAndOpenSeeks(t *testing.T) {
	t.Parallel()

	driver, err := New()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, driver.Put(ctx, "a/b.txt", bytes.NewReader([]byte("0123456789")), 0, ""))
	longer := driver.Put(ctx, "a/c.txt", bytes.NewReader([]byte("0123456789")), 4, "")
	require.Error(t, longer, "a longer stream is not cut")

	file, info, err := driver.Open(ctx, "a/b.txt")
	require.NoError(t, err)

	defer func() { _ = file.Close() }()

	assert.EqualValues(t, 10, info.Size())
	assert.Equal(t, "b.txt", info.Name())
	assert.False(t, info.ModTime().IsZero())

	_, err = file.Seek(8, io.SeekStart)
	require.NoError(t, err)

	rest, _ := io.ReadAll(file)
	assert.Equal(t, "89", string(rest))

	require.NoError(t, driver.Delete(ctx, "missing"))

	keys, err := driver.List(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, []string{"a/b.txt"}, keys)
}
