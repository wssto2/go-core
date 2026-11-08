package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	htmpl "text/template"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

// StateBuilder builds request-scoped state that is injected into the SPA shell.
// An error answers 503 with a plain page (the cause stays out of it) and is
// reported like a handler error: logged, and an incident when incidents are installed.
type StateBuilder func(ctx *gin.Context) (any, error)

// StateBuilderHTTP is StateBuilder on net/http: the request is the one the shell answers. Prefer it to StateBuilder.
type StateBuilderHTTP func(r *http.Request) (any, error)

// AppStateBuilder is kept as an alias for the SPA state builder type.
type AppStateBuilder = StateBuilder

// unavailablePage is the 503 answer when the SPA state cannot be built.
const unavailablePage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Unavailable</title></head>` +
	`<body><h1>Temporarily unavailable</h1><p>Please try again in a moment.</p></body></html>`

// SPAConfig configures the SPA catch-all handler and template loading.
type SPAConfig struct {
	// Vite holds Vite dev server and asset resolution config.
	Vite ViteConfig

	// FS, when set, holds the templates and the built files instead of the disk: TemplatesPath and Vite.ManifestPath are
	// read in it (slash-separated, "./" allowed), and the files under the manifest's build directory are served from it.
	// An app embeds them: //go:embed frontend/dist frontend/templates, FS: embedded. Nil reads the paths from disk.
	FS fs.FS

	// TemplatesPath is the glob used to load HTML templates (in FS, when set).
	// Defaults to "frontend/templates/*.html".
	TemplatesPath string

	// TemplateName is the HTML template to render for all non-API routes.
	// Defaults to "index.html".
	TemplateName string

	// APIPrefix is the URL prefix that must NOT be caught by NoRoute.
	// Defaults to "/api".
	APIPrefix string

	// BasePath, when set, limits the shell to that path and what is under it ("/app"); any other path that is
	// not under APIPrefix goes to Fallthrough. Empty: the shell answers every path outside APIPrefix.
	BasePath string

	// Fallthrough answers the paths outside BasePath; nil answers them 404 like the API prefix.
	Fallthrough gin.HandlerFunc

	// StateBuilder is optional. If provided, its return value is exposed to the
	// template as appState.
	StateBuilder StateBuilder

	// StateBuilderHTTP is StateBuilder without gin, and is used instead of it when both are set.
	StateBuilderHTTP StateBuilderHTTP

	// ExtraFuncs allows the application to add template functions on top of the
	// built-in toJSON helper.
	ExtraFuncs htmpl.FuncMap

	// DevMode resolves assets on every request instead of once at startup.
	// In development this should usually be true.
	DevMode bool
}

// Owns says whether the shell side answers path: the API prefix (with its 404) or the base path. A config with no
// BasePath owns every path. Call it on a config with defaults applied.
func (c SPAConfig) Owns(path string) bool {
	base := strings.TrimRight(c.BasePath, "/")

	return strings.HasPrefix(path, c.APIPrefix) || c.BasePath == "" || path == base || strings.HasPrefix(path, base+"/")
}

func (c SPAConfig) withDefaults() SPAConfig {
	if c.TemplatesPath == "" {
		c.TemplatesPath = "frontend/templates/*.html"
	}

	if c.TemplateName == "" {
		c.TemplateName = "index.html"
	}

	if c.APIPrefix == "" {
		c.APIPrefix = "/api"
	}

	if c.Vite.Entry == "" {
		c.Vite.Entry = "src/main.js"
	}

	if c.Vite.ManifestPath == "" {
		c.Vite.ManifestPath = "./frontend/dist/.vite/manifest.json"
	}

	if c.Vite.AssetsURLPrefix == "" {
		c.Vite.AssetsURLPrefix = "/frontend/dist"
	}

	return c
}

// BuiltinFuncMap returns the template functions go-core always provides.
// Applications can merge extra functions via SPAConfig.ExtraFuncs.
func BuiltinFuncMap() htmpl.FuncMap {
	return htmpl.FuncMap{
		"toJSON": func(v any) template.JS {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(v); err != nil {
				return template.JS("null")
			}
			return template.JS(bytes.TrimRight(buf.Bytes(), "\n"))
		},
	}
}

// RegisterSPA wires template loading and the NoRoute catch-all handler on the
// given engine. Call this after all other routes have been registered.
//
// The handler:
//   - Returns 404 JSON for any request under APIPrefix.
//   - Renders TemplateName for everything else, with template data:
//     .Assets   — frontend.Assets
//     .AppState — whatever StateBuilder returns (nil if not configured)
//     .Nonce    — CSP nonce if present in gin.Context
func RegisterSPA(engine *gin.Engine, cfg SPAConfig, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	cfg = cfg.withDefaults()

	funcs := BuiltinFuncMap()
	for k, v := range cfg.ExtraFuncs {
		funcs[k] = v
	}
	engine.SetFuncMap(funcs)

	if cfg.FS != nil {
		glob := path.Clean(filepath.ToSlash(cfg.TemplatesPath))
		engine.SetHTMLTemplate(template.Must(template.New("").Funcs(funcs).ParseFS(cfg.FS, glob)))
	} else {
		engine.LoadHTMLGlob(cfg.TemplatesPath)
	}

	var productionAssets *Assets
	if !cfg.DevMode {
		a := resolveAssets(cfg.Vite, cfg.FS)
		if !a.IsDev {
			productionAssets = &a
		}
	}

	log.Info("frontend: SPA registered",
		"template", cfg.TemplateName,
		"templates_path", cfg.TemplatesPath,
		"api_prefix", cfg.APIPrefix,
		"dev_mode", cfg.DevMode,
	)

	engine.NoRoute(func(ctx *gin.Context) {
		if !cfg.Owns(ctx.Request.URL.Path) {
			if cfg.Fallthrough == nil {
				ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			} else {
				cfg.Fallthrough(ctx)
			}

			return
		}

		if strings.HasPrefix(ctx.Request.URL.Path, cfg.APIPrefix) {
			ctx.JSON(404, gin.H{"error": "not found"})
			return
		}

		assets := productionAssets
		if assets == nil {
			a := resolveAssets(cfg.Vite, cfg.FS)
			assets = &a
		}

		var appState any

		if cfg.StateBuilder != nil || cfg.StateBuilderHTTP != nil {
			var (
				state any
				err   error
			)

			if cfg.StateBuilderHTTP != nil {
				state, err = cfg.StateBuilderHTTP(ctx.Request)
			} else {
				state, err = cfg.StateBuilder(ctx)
			}

			if err != nil {
				_ = ctx.Error(apperr.Internal(fmt.Errorf("frontend: building the SPA state: %w", err)))
				ctx.Data(http.StatusServiceUnavailable, "text/html; charset=utf-8", []byte(unavailablePage))
				ctx.Abort()

				return
			}

			appState = state
		}

		nonce, _ := ctx.Get("nonce")
		log.Info("frontend: SPA rendering", "path", ctx.Request.URL.Path, "nonce", nonce)

		ctx.HTML(200, cfg.TemplateName, gin.H{
			"Assets":   assets,
			"AppState": appState,
			"Nonce":    nonce,
		})
	})
}

// RegisterSPAWithTemplates is an alias for RegisterSPA.
// SPA registration now owns template setup for the common case.
func RegisterSPAWithTemplates(engine *gin.Engine, cfg SPAConfig, log *slog.Logger) {
	RegisterSPA(engine, cfg, log)
}

// BuiltFiles returns the build directory of the Vite output (two levels above the manifest, from FS when set) to serve
// under Vite.AssetsURLPrefix, and says which of its files the manifest names: entries, their CSS and assets, whose
// names Vite hashes by content. The manifest is read once; a missing one names nothing.
func (c SPAConfig) BuiltFiles() (http.FileSystem, func(name string) bool, error) {
	vite := c.Vite.WithDefaults()
	named := map[string]bool{}

	manifest, err := readManifest(c.FS, vite.ManifestPath)
	if err == nil {
		for _, entry := range manifest {
			for _, name := range append(append([]string{entry.File}, entry.CSS...), entry.Assets...) {
				named[strings.TrimLeft(filepath.ToSlash(name), "/")] = true
			}
		}
	}

	isNamed := func(name string) bool { return named[strings.TrimLeft(name, "/")] }

	if c.FS == nil {
		return http.Dir(filepath.Dir(filepath.Dir(vite.ManifestPath))), isNamed, nil
	}

	sub, err := fs.Sub(c.FS, path.Dir(path.Dir(path.Clean(filepath.ToSlash(vite.ManifestPath)))))
	if err != nil {
		return nil, nil, fmt.Errorf("frontend: the build directory in FS: %w", err)
	}

	return http.FS(sub), isNamed, nil
}
