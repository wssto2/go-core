package gocore_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/storage"
)

func newDisksApp() *gocore.App {
	return gocore.New(gocore.WithLogger(slog.New(slog.DiscardHandler)))
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
