//nolint:testpackage // tests the unexported lifecycle and builder gocore is built on
//nolint:testpackage // serves through the unexported serveStatic, the one place both static mounts go through
package gocore

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeStaticHidesSourceMaps(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "main.js"), []byte("console.log(1)"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "main.js.map"), []byte(`{"sources":["main.ts"]}`), 0o600))

	engine := gin.New()
	serveStatic(engine, "/dist", dir)

	for path, want := range map[string]int{
		"/dist/assets/main.js":     http.StatusOK,
		"/dist/assets/main.js.map": http.StatusNotFound,
		"/dist/assets/absent.js":   http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		assert.Equal(t, want, rec.Code, path)
	}
}
