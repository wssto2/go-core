package testhelpers

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/wssto2/go-core/storage/memory"
)

// NewMemoryTempDriver creates a memory storage driver backed by a temporary directory.
func NewMemoryTempDriver(t *testing.T) (*memory.Driver, func()) {
	t.Helper()
	d, err := memory.New()
	if err != nil {
		t.Fatalf("failed to create memory driver: %v", err)
	}
	return d, func() {}
}

// WriteString writes content to the driver at key. Useful for test setup.
func WriteString(t *testing.T, d *memory.Driver, key, content string) {
	t.Helper()
	r := io.NopCloser(strings.NewReader(content))
	if err := d.Put(context.TODO(), key, r, int64(len(content)), "text/plain"); err != nil {
		t.Fatalf("failed to put content: %v", err)
	}
}
