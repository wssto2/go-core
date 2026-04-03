package memory

import (
	"bytes"
	"context"
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
