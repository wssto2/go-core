package web_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/storage"
	"github.com/wssto2/go-core/storage/memory"
	"github.com/wssto2/go-core/web"
)

// plain hides Open: a driver without the seek capability.
type plain struct{ storage.Driver }

func serveEngine(t *testing.T, disk storage.Driver) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, false))
	engine.GET("/files/*key", func(c *gin.Context) {
		key := c.Param("key")[1:]
		web.Serve(c, disk, key, web.Attachment("Ugovor č.pdf"))
	})

	return engine
}

func do(engine *gin.Engine, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/files/docs/a.pdf", nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	return rec
}

func mustMemory(t *testing.T) *memory.Driver {
	t.Helper()

	d, err := memory.New()
	require.NoError(t, err)

	return d
}

func TestServeStreamsWithRangeAndLastModified(t *testing.T) {
	t.Parallel()

	reg := storage.NewRegistry()
	require.NoError(t, reg.Add(storage.Disk("d").Local(t.TempDir())))
	disk, _ := reg.Disk("d")
	require.NoError(t, disk.Put(context.Background(), "docs/a.pdf", bytes.NewBufferString("0123456789"), 0, ""))
	engine := serveEngine(t, disk)

	rec := do(engine)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "0123456789", rec.Body.String())
	assert.Equal(t, "application/pdf", rec.Header().Get("Content-Type"))
	assert.Equal(t, "bytes", rec.Header().Get("Accept-Ranges"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "č", "the header carries the name")

	last := rec.Header().Get("Last-Modified")
	require.NotEmpty(t, last)

	part := do(engine, "Range", "bytes=2-4")
	assert.Equal(t, http.StatusPartialContent, part.Code)
	assert.Equal(t, "234", part.Body.String())
	assert.Equal(t, "bytes 2-4/10", part.Header().Get("Content-Range"))

	cached := do(engine, "If-Modified-Since", last)
	assert.Equal(t, http.StatusNotModified, cached.Code)
	assert.Empty(t, cached.Body.String())

	old := do(engine, "If-Modified-Since", time.Unix(0, 0).UTC().Format(http.TimeFormat))
	assert.Equal(t, http.StatusOK, old.Code)
}

func TestServeMissingFileAnswersTheJSON404(t *testing.T) {
	t.Parallel()

	reg := storage.NewRegistry()
	require.NoError(t, reg.Add(storage.Disk("d").Memory()))
	disk, _ := reg.Disk("d")

	for name, d := range map[string]storage.Driver{"seekable": disk, "plain": plain{disk}} {
		rec := do(serveEngine(t, d))
		assert.Equal(t, http.StatusNotFound, rec.Code, name)
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json", name)
		assert.NotContains(t, rec.Body.String(), "docs/a.pdf", "the key is not echoed (%s)", name)
		assert.Empty(t, rec.Header().Get("Content-Disposition"), name)
	}
}

func TestServeFallsBackToAPlainStreamWithoutSeek(t *testing.T) {
	t.Parallel()

	reg := storage.NewRegistry()
	require.NoError(t, reg.Add(storage.Disk("d").Memory()))
	disk, _ := reg.Disk("d")
	require.NoError(t, disk.Put(context.Background(), "docs/a.pdf", bytes.NewBufferString("0123456789"), 0, ""))

	rec := do(serveEngine(t, plain{disk}), "Range", "bytes=2-4")
	assert.Equal(t, http.StatusOK, rec.Code, "no seek, no range")
	assert.Equal(t, "0123456789", rec.Body.String())
	assert.Equal(t, "application/pdf", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
}

func TestServeThroughAFilesWithoutSeekFallsBack(t *testing.T) {
	t.Parallel()

	reg := storage.NewRegistry()
	require.NoError(t, reg.Add(storage.Disk("d").Driver(plain{mustMemory(t)})))
	disk, _ := reg.Disk("d")
	require.NoError(t, disk.Put(context.Background(), "docs/a.pdf", bytes.NewBufferString("abc"), 0, ""))

	rec := do(serveEngine(t, disk))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "abc", rec.Body.String())
}

func TestInlineAndAttachmentCarryTheName(t *testing.T) {
	t.Parallel()

	assert.Contains(t, web.Inline("x.png"), "inline")
	assert.Contains(t, web.Inline("x.png"), "x.png")
	assert.Contains(t, web.Attachment("x.png"), "attachment")
	assert.Contains(t, web.Attachment("x.png"), "x.png")
}
