package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	htmpl "text/template"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
)

// StateBuilder builds request-scoped state that is injected into the SPA shell.
// An error answers 503 with a plain page (the cause stays out of it) and is
// reported like a handler error: logged, and an incident when incidents are installed.
type StateBuilder func(ctx *gin.Context) (any, error)

// AppStateBuilder is kept as an alias for the SPA state builder type.
type AppStateBuilder = StateBuilder

// unavailablePage is the 503 answer when the SPA state cannot be built.
const unavailablePage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Unavailable</title></head>` +
	`<body><h1>Temporarily unavailable</h1><p>Please try again in a moment.</p></body></html>`

// SPAConfig configures the SPA catch-all handler and template loading.
type SPAConfig struct {
	// Vite holds Vite dev server and asset resolution config.
	Vite ViteConfig

	// TemplatesPath is the glob used to load HTML templates.
	// Defaults to "frontend/templates/*.html".
	TemplatesPath string

	// TemplateName is the HTML template to render for all non-API routes.
	// Defaults to "index.html".
	TemplateName string

	// APIPrefix is the URL prefix that must NOT be caught by NoRoute.
	// Defaults to "/api".
	APIPrefix string

	// StateBuilder is optional. If provided, its return value is exposed to the
	// template as appState.
	StateBuilder StateBuilder

	// ExtraFuncs allows the application to add template functions on top of the
	// built-in toJSON helper.
	ExtraFuncs htmpl.FuncMap

	// DevMode resolves assets on every request instead of once at startup.
	// In development this should usually be true.
	DevMode bool
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
	engine.LoadHTMLGlob(cfg.TemplatesPath)

	var productionAssets *Assets
	if !cfg.DevMode {
		a := ResolveAssets(cfg.Vite)
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
		if strings.HasPrefix(ctx.Request.URL.Path, cfg.APIPrefix) {
			ctx.JSON(404, gin.H{"error": "not found"})
			return
		}

		assets := productionAssets
		if assets == nil {
			a := ResolveAssets(cfg.Vite)
			assets = &a
		}

		var appState any
		if cfg.StateBuilder != nil {
			state, err := cfg.StateBuilder(ctx)
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
