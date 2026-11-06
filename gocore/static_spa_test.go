//nolint:testpackage,exhaustruct,exhaustruct_v5,paralleltest,goconst,gochecknoglobals,lll,forcetypeassert // tests the unexported builder gocore is built on
package gocore

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/frontend"
)

const (
	spaPage     = `<html><body>shell {{ .Assets.JSPath }}</body></html>`
	spaManifest = `{"main.ts":{"file":"assets/main-abc.js","css":["assets/main-def.css"],"assets":["assets/logo-123.svg"],"isEntry":true}}`
)

var spaFiles = map[string]string{
	"frontend/templates/index.html":        spaPage,
	"frontend/dist/.vite/manifest.json":    spaManifest,
	"frontend/dist/assets/main-abc.js":     "js",
	"frontend/dist/assets/main-def.css":    "css",
	"frontend/dist/assets/logo-123.svg":    "svg",
	"frontend/dist/favicon.ico":            "ico",
	"frontend/dist/assets/main-abc.js.map": "map",
}

// spaApp builds an app with the SPA under /admin whose files are read from disk (dir) or from an fs.FS (fsys).
func spaApp(t *testing.T, fromFS bool) *lifecycle {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := DefaultConfig()
	cfg.App.Env = "production"
	cfg.I18n.Dir = tempI18nDir(t)
	spa := frontend.SPAConfig{
		TemplatesPath: "frontend/templates/*.html", BasePath: "/admin", APIPrefix: "/admin/api",
		Vite: frontend.ViteConfig{Entry: "main.ts", ManifestPath: "./frontend/dist/.vite/manifest.json", AssetsURLPrefix: "/admin/assets"},
	}

	if fromFS {
		spa.FS = fstest.MapFS{}
		for name, body := range spaFiles {
			spa.FS.(fstest.MapFS)[name] = &fstest.MapFile{Data: []byte(body)}
		}
	} else {
		root := t.TempDir()
		for name, body := range spaFiles {
			require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o600))
		}

		spa.TemplatesPath = filepath.Join(root, spa.TemplatesPath)
		spa.Vite.ManifestPath = filepath.Join(root, "frontend/dist/.vite/manifest.json")
	}

	app, err := newBuilder(cfg).DefaultInfrastructure().WithSPAConfig(spa).
		Use(func(ctx *gin.Context) { ctx.Header("X-Seen", "use") }).WithHTTP().Build()
	require.NoError(t, err)

	return app
}

func spaGet(t *testing.T, app *lifecycle, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	app.engine.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

	return rec
}

func TestSPAStaticFilesDiskAndFS(t *testing.T) {
	for name, fromFS := range map[string]bool{"disk": false, "fs": true} {
		t.Run(name, func(t *testing.T) {
			app := spaApp(t, fromFS)

			for path, want := range map[string]struct {
				code  int
				cache string
			}{
				"/admin/assets/assets/main-abc.js":     {200, immutableCache},
				"/admin/assets/assets/main-def.css":    {200, immutableCache},
				"/admin/assets/assets/logo-123.svg":    {200, immutableCache},
				"/admin/assets/favicon.ico":            {200, ""}, // not named by the manifest: not hashed
				"/admin/assets/assets/main-abc.js.map": {404, ""},
				"/admin/assets/assets/nope.js":         {404, ""},
				"/admin/assets/assets/":                {404, ""},
			} {
				rec := spaGet(t, app, path)
				assert.Equal(t, want.code, rec.Code, path)
				assert.Equal(t, want.cache, rec.Header().Get("Cache-Control"), path)
				assert.NotContains(t, rec.Body.String(), "shell", path+" is not the SPA page")
				assert.Equal(t, "use", rec.Header().Get("X-Seen"), path+": App.Use reaches the static routes")
			}

			page := spaGet(t, app, "/admin/some/screen")
			assert.Equal(t, http.StatusOK, page.Code)
			assert.Contains(t, page.Body.String(), "shell /admin/assets/assets/main-abc.js")
		})
	}
}
