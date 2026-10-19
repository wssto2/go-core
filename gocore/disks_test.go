package gocore_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/storage"
)

func newDisksApp() *gocore.App {
	return gocore.New(gocore.WithConfig(gocore.DefaultConfig()), gocore.WithLogger(slog.New(slog.DiscardHandler)))
}

func TestDisksDeclareNamedDisksThatFeaturesTake(t *testing.T) {
	t.Parallel()

	app := newDisksApp()
	root := t.TempDir()

	app.Disks(
		storage.Disk("uploads").Local(root+"/uploads"),
		storage.Disk("public").Local(root+"/public").Public("/media"),
	)

	documents := storage.Use(app, "uploads").Folder("vehicle-documents")
	photos := storage.Use(app, "public").Folder("vehicles")

	ctx := context.Background()
	require.NoError(t, photos.Put(ctx, "123/photos/front.jpg", strings.NewReader("jpg"), 0, "image/jpeg"))
	require.NoError(t, documents.Put(ctx, "a.pdf", strings.NewReader("pdf"), 0, "application/pdf"))

	url, err := photos.URL(ctx, "123/photos/front.jpg")
	require.NoError(t, err)
	assert.Equal(t, "/media/vehicles/123/photos/front.jpg", url)

	_, err = documents.URL(ctx, "a.pdf")
	require.Error(t, err)

	require.NoError(t, app.Check())
}

func TestAnUnknownOrBrokenDiskStopsStartUp(t *testing.T) {
	t.Parallel()

	app := newDisksApp()

	app.Disks(storage.Disk("uploads").Memory(), storage.Disk("uploads").Memory(), storage.Disk("lost"))
	storage.Use(app, "uploadz")

	err := app.Check()
	require.Error(t, err)

	msg := err.Error()
	assert.Contains(t, msg, `"uploads" is declared twice`)
	assert.Contains(t, msg, `"lost"`)
	assert.Contains(t, msg, `no disk is declared as "uploadz"`)
}

func TestAPublicPathDiskIsServedReadOnly(t *testing.T) {
	t.Parallel()

	app := newDisksApp()
	root := t.TempDir()
	app.Disks(storage.Disk("uploads").Local(root+"/uploads"), storage.Disk("public").Local(root+"/public").Public("/media"))

	photos := storage.Use(app, "public").Folder("vehicles")
	require.NoError(t, photos.Put(context.Background(), "123/front.jpg", strings.NewReader("jpeg bytes"), 10, "image/jpeg"))
	require.NoError(t, storage.Use(app, "uploads").Put(context.Background(), "secret.pdf", strings.NewReader("pdf"), 3, "application/pdf"))

	handler, err := app.Handler()
	require.NoError(t, err)

	do := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, path, nil))

		return rec
	}

	file := do(http.MethodGet, "/media/vehicles/123/front.jpg")
	assert.Equal(t, http.StatusOK, file.Code)
	assert.Equal(t, "jpeg bytes", file.Body.String())
	assert.Equal(t, "image/jpeg", file.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, do(http.MethodHead, "/media/vehicles/123/front.jpg").Code)
	assert.Equal(t, http.StatusNotFound, do(http.MethodGet, "/media/vehicles/missing.jpg").Code)
	assert.Equal(t, http.StatusNotFound, do(http.MethodPost, "/media/vehicles/123/front.jpg").Code)

	for _, path := range []string{
		"/media/vehicles/123", "/media/vehicles/123/", "/media/", "/media/vehicles/../uploads/secret.pdf",
		"/media/%2e%2e/uploads/secret.pdf", "/media//etc/passwd",
	} {
		rec := do(http.MethodGet, path)
		assert.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, path)
		assert.NotContains(t, rec.Body.String(), "pdf", path)
	}
}

func TestAnAbsoluteURLDiskIsNotMounted(t *testing.T) {
	t.Parallel()

	app := newDisksApp()
	app.Disks(storage.Disk("public").Local(t.TempDir()).Public("https://cdn.example.com/media"))
	require.NoError(t, storage.Use(app, "public").Put(context.Background(), "a.jpg", strings.NewReader("x"), 1, "image/jpeg"))

	handler, err := app.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/media/a.jpg", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
