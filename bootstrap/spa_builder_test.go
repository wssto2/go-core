package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/frontend"
)

func TestWithSPA_UsesConventionDefaultsAndRendersState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	defer func() {
		_ = os.Chdir(oldWD)
	}()

	templateDir := filepath.Join(tempDir, "frontend", "templates")
	manifestDir := filepath.Join(tempDir, "frontend", "dist", ".vite")

	require.NoError(t, os.MkdirAll(templateDir, 0o755))
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

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

	manifest := `{
		"src/main.ts": {
			"file": "assets/main-abc123.js",
			"css": ["assets/main-def456.css"],
			"isEntry": true
		}
	}`
	require.NoError(t, os.WriteFile(
		filepath.Join(manifestDir, "manifest.json"),
		[]byte(manifest),
		0o644,
	))

	cfg := DefaultConfig()
	cfg.App.Env = "production"
	cfg.I18n.Dir = tempI18nDir(t)

	builder := New(cfg).DefaultInfrastructure().WithSPA(func(ctx *gin.Context) any {
		return gin.H{
			"path": ctx.Request.URL.Path,
		}
	})
	builder.spaConfig.Vite.DevServerURL = "http://127.0.0.1:65534"

	app, err := builder.Build()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	app.engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()

	assert.Contains(t, body, `/frontend/dist/assets/main-abc123.js`)
	assert.Contains(t, body, `/frontend/dist/assets/main-def456.css`)
	assert.Contains(t, body, `window.APP_STATE = {"path":"/dashboard"}`)
}

func TestWithSPA_DefaultsAPIPrefixToSlashAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	defer func() {
		_ = os.Chdir(oldWD)
	}()

	templateDir := filepath.Join(tempDir, "frontend", "templates")
	require.NoError(t, os.MkdirAll(templateDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(templateDir, "index.html"),
		[]byte(`<!doctype html><html><body><div id="app"></div></body></html>`),
		0o644,
	))

	cfg := DefaultConfig()
	cfg.App.Env = "production"
	cfg.I18n.Dir = tempI18nDir(t)
	cfg.Frontend.APIPrefix = ""

	builder := New(cfg).DefaultInfrastructure().WithSPA(nil)

	app, err := builder.Build()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	w := httptest.NewRecorder()
	app.engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	assert.JSONEq(t, `{"error":"not found"}`, w.Body.String())
}

func TestWithSPAConfig_UsesExplicitConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	templateDir := filepath.Join(tempDir, "custom-templates")
	manifestDir := filepath.Join(tempDir, "custom-dist", ".vite")

	require.NoError(t, os.MkdirAll(templateDir, 0o755))
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	templateContent := `<!doctype html>
<html>
  <body>
    <div id="app"></div>
    {{ if .AppState }}
    <script>
      window.APP_STATE = {{ .AppState | toJSON }};
    </script>
    {{ end }}
    <script type="module" src="{{ .Assets.JSPath }}"></script>
  </body>
</html>`
	require.NoError(t, os.WriteFile(
		filepath.Join(templateDir, "shell.html"),
		[]byte(templateContent),
		0o644,
	))

	manifest := `{
		"src/custom.ts": {
			"file": "assets/custom-123.js",
			"css": [],
			"isEntry": true
		}
	}`
	manifestPath := filepath.Join(manifestDir, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifest), 0o644))

	cfg := DefaultConfig()
	cfg.App.Env = "production"
	cfg.I18n.Dir = tempI18nDir(t)

	builder := New(cfg).DefaultInfrastructure().WithSPAConfig(frontend.SPAConfig{
		TemplatesPath: filepath.Join(templateDir, "*.html"),
		TemplateName:  "shell.html",
		APIPrefix:     "/backend",
		StateBuilder: func(ctx *gin.Context) any {
			return gin.H{"ok": true}
		},
		Vite: frontend.ViteConfig{
			DevServerURL:    "http://127.0.0.1:65534",
			Entry:           "src/custom.ts",
			ManifestPath:    manifestPath,
			AssetsURLPrefix: "/public/assets",
		},
		DevMode: false,
	})

	app, err := builder.Build()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	app.engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()

	assert.Contains(t, body, `/public/assets/assets/custom-123.js`)
	assert.Contains(t, body, `window.APP_STATE = {"ok":true}`)
}
