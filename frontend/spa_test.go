//nolint:err113,exhaustruct,exhaustruct_v5,noctx,noinlineerr,paralleltest,varnamelen // tests in the style of the file
package frontend

import (
	"bytes"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/middlewares"
)

func TestRegisterSPA_NilLogger_NoPanic(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	templateDir := filepath.Join(tempDir, "frontend", "templates")
	require.NoError(t, os.MkdirAll(templateDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(templateDir, "index.html"),
		[]byte(`<!doctype html><html><body><div id="app"></div></body></html>`),
		0o644,
	))

	engine := gin.New()
	cfg := SPAConfig{
		TemplatesPath: filepath.Join(templateDir, "*.html"),
		APIPrefix:     "/api",
	}

	require.NotPanics(t, func() {
		RegisterSPA(engine, cfg, nil)
	}, "RegisterSPA must not panic with a nil logger")

	req := httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	w := httptest.NewRecorder()

	require.NotPanics(t, func() {
		engine.ServeHTTP(w, req)
	}, "NoRoute handler must not panic with a nil logger")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.JSONEq(t, `{"error":"not found"}`, w.Body.String())
}

func TestBuiltinFuncMap_ToJSON_ReturnsTemplateJS(t *testing.T) {
	funcs := BuiltinFuncMap()
	toJSON, ok := funcs["toJSON"]
	require.True(t, ok, "toJSON must be in BuiltinFuncMap")

	type testVal struct {
		Name string `json:"name"`
	}
	result := toJSON.(func(any) template.JS)(testVal{Name: "hello<world>"})

	assert.Contains(t, string(result), "<world>", "< must not be escaped in template.JS output")
	assert.Contains(t, string(result), `"name":"hello<world>"`, "JSON output must be valid JSON")
}

func TestResolveAssets_ManifestBasedProductionAssets(t *testing.T) {
	tempDir := t.TempDir()
	manifestDir := filepath.Join(tempDir, "frontend", "dist", ".vite")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	manifest := `{
		"src/main.ts": {
			"file": "assets/main-abc123.js",
			"css": ["assets/main-def456.css"],
			"isEntry": true
		}
	}`
	manifestPath := filepath.Join(manifestDir, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifest), 0o644))

	assets := ResolveAssets(ViteConfig{
		DevServerURL:    "http://127.0.0.1:65534",
		Entry:           "src/main.ts",
		ManifestPath:    manifestPath,
		AssetsURLPrefix: "/frontend/dist",
	})

	assert.False(t, assets.IsDev)
	assert.Equal(t, "/frontend/dist/assets/main-abc123.js", assets.JSPath)
	assert.Equal(t, []string{"/frontend/dist/assets/main-def456.css"}, assets.CSSPaths)
	assert.Empty(t, assets.ViteClientURL)
}

func TestRegisterSPA_RendersManifestAssetsAndAppState(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()

	templateDir := filepath.Join(tempDir, "frontend", "templates")
	require.NoError(t, os.MkdirAll(templateDir, 0o755))
	templateContent := `<!doctype html>
<html>
  <head>
    {{ range .Assets.CSSPaths }}
    <link rel="stylesheet" href="{{ . }}" />
    {{ end }}
  </head>
  <body>
    <div id="app"></div>
    {{ if .AppState }}
    <script nonce="{{ .Nonce }}">
      window.APP_STATE = {{ .AppState | toJSON }};
    </script>
    {{ end }}
    {{ if .Assets.ViteClientURL }}
    <script type="module" src="{{ .Assets.ViteClientURL }}"></script>
    {{ end }}
    <script type="module" src="{{ .Assets.JSPath }}"></script>
  </body>
</html>`
	require.NoError(t, os.WriteFile(
		filepath.Join(templateDir, "index.html"),
		[]byte(templateContent),
		0o644,
	))

	manifestDir := filepath.Join(tempDir, "frontend", "dist", ".vite")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))
	manifest := `{
		"src/main.ts": {
			"file": "assets/main-abc123.js",
			"css": ["assets/main-def456.css"],
			"isEntry": true
		}
	}`
	manifestPath := filepath.Join(manifestDir, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifest), 0o644))

	engine := gin.New()
	engine.Use(func(ctx *gin.Context) {
		ctx.Set("nonce", "test-nonce")
		ctx.Next()
	})

	RegisterSPA(engine, SPAConfig{
		TemplatesPath: filepath.Join(templateDir, "*.html"),
		TemplateName:  "index.html",
		APIPrefix:     "/api",
		DevMode:       false,
		Vite: ViteConfig{
			DevServerURL:    "http://127.0.0.1:65534",
			Entry:           "src/main.ts",
			ManifestPath:    manifestPath,
			AssetsURLPrefix: "/frontend/dist",
		},
		StateBuilder: func(ctx *gin.Context) (any, error) {
			return gin.H{
				"path": ctx.Request.URL.Path,
			}, nil
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()

	assert.Contains(t, body, `/frontend/dist/assets/main-abc123.js`)
	assert.Contains(t, body, `/frontend/dist/assets/main-def456.css`)
	assert.Contains(t, body, `window.APP_STATE = {"path":"/dashboard"}`)
	assert.Contains(t, body, `nonce="test-nonce"`)
}

func TestResolveAssets_DevModeUsesViteClientProbe(t *testing.T) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/@vite/client", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/src/main.ts", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	assets := ResolveAssets(ViteConfig{
		DevServerURL: server.URL,
		Entry:        "src/main.ts",
	})

	assert.True(t, assets.IsDev)
	assert.Equal(t, server.URL+"/@vite/client", assets.ViteClientURL)
	assert.Equal(t, server.URL+"/src/main.ts", assets.JSPath)
	assert.Empty(t, assets.CSSPaths)
}

func TestRegisterSPA_StateBuilderErrorAnswers503PlainPageAndIsReported(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<html>{{ .AppState }}</html>`), 0o600))

	var logged bytes.Buffer

	log := slog.New(slog.NewJSONHandler(&logged, nil))

	var reported error

	engine := gin.New()
	engine.Use(func(ctx *gin.Context) {
		ctx.Next()

		if last := ctx.Errors.Last(); last != nil {
			reported = last.Err
		}
	})
	engine.Use(middlewares.ErrorHandler(log, nil, false))

	RegisterSPA(engine, SPAConfig{
		TemplatesPath: filepath.Join(dir, "*.html"),
		TemplateName:  "index.html",
		DevMode:       true,
		StateBuilder:  func(*gin.Context) (any, error) { return nil, errors.New("the session store is down") },
	}, log)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard", nil))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.NotContains(t, w.Body.String(), "session store", "the cause stays out of the page")
	assert.NotContains(t, w.Body.String(), `{"`, "the page is not followed by a JSON error body")
	require.Error(t, reported, "the error is reported like a handler error")
	assert.Contains(t, reported.Error(), "the session store is down")
	assert.Contains(t, logged.String(), "the session store is down")
}
