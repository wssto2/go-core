package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/wssto2/go-core/apperr"
)

func TestLocalDriver_PutGetDelete(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "localdriver_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpdir) }()

	d, err := New(tmpdir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	key := "sub/dir/test.txt"
	content := []byte("hello world")
	if err := d.Put(ctx, key, bytes.NewReader(content), int64(len(content)), "text/plain"); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	r, err := d.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Fatalf("content mismatch: got %q expected %q", string(data), string(content))
	}

	url, err := d.URL(ctx, key)
	if err != nil {
		t.Fatalf("URL failed: %v", err)
	}
	if url == "" {
		t.Fatalf("empty url")
	}

	if err := d.Delete(ctx, key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = d.Get(ctx, key)
	if err == nil {
		t.Fatalf("expected error after delete")
	}
	var aerr *apperr.AppError
	if !errors.As(err, &aerr) {
		t.Fatalf("expected apperr.AppError, got %T", err)
	}
	if aerr.Code != apperr.CodeNotFound {
		t.Fatalf("expected CodeNotFound, got %s", aerr.Code)
	}
}

func TestLocalDriver_ListAndExists(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "localdriver_list_exists_test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpdir); err != nil {
			t.Fatalf("RemoveAll: %v", err)
		}
	}()

	d, err := New(tmpdir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	files := []struct {
		key string
		val []byte
	}{
		{"a/1.txt", []byte("one")},
		{"a/2.txt", []byte("two")},
		{"b/3.txt", []byte("three")},
	}
	for _, f := range files {
		if err := d.Put(ctx, f.key, bytes.NewReader(f.val), int64(len(f.val)), "text/plain"); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	keys, err := d.List(ctx, "a/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}

	exists, err := d.Exists(ctx, "a/1.txt")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected Exists to return true for a/1.txt")
	}

	notExists, err := d.Exists(ctx, "notfound.txt")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if notExists {
		t.Fatalf("expected Exists to return false for notfound.txt")
	}
}
