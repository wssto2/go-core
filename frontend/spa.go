package frontend

import (
	"bytes"
	"encoding/json"
	"html/template"
	"log/slog"
	"strings"
	htmpl "text/template"

	"github.com/gin-gonic/gin"
)

// SPAConfig configures the SPA catch-all handler.
type SPAConfig struct {
	// Vite holds Vite dev server and asset resolution config.
	Vite ViteConfig

	TemplatesPath string // e.g. "templates/*.html"

	// TemplateName is the HTML template to render for all non-API routes.
	// Defaults to "index.html".
	TemplateName string

	// APIPrefix is the URL prefix that must NOT be caught by NoRoute.
	// Defaults to "/api".
	APIPrefix string

	// StateBuilder is optional. If provided, its return value is
	// serialised as window.APP_STATE in the template.
	// If nil, window.APP_STATE is not injected.
	StateBuilder AppStateBuilder

	// ExtraFuncs allows the application to add template functions
	// on top of the built-in toJSON that go-core provides.
	ExtraFuncs htmpl.FuncMap

	// DevMode resolves assets on every request instead of once at startup.
	// Set to true when Env is "development" so Vite start/stop is detected automatically.
	// In production this should always be false.
	DevMode bool
}

func (c SPAConfig) withDefaults() SPAConfig {
	if c.TemplatesPath == "" {
		c.TemplatesPath = "templates/*.html"
	}

	if c.TemplateName == "" {
		c.TemplateName = "index.html"
	}

	if c.APIPrefix == "" {
		c.APIPrefix = "/api"
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
			// Encode appends a trailing newline; trim it.
			return template.JS(bytes.TrimRight(buf.Bytes(), "\n"))
		},
	}
}

// RegisterSPA wires the NoRoute catch-all handler on the given engine.
// Call this after all other routes have been registered.
//
// The handler:
//   - Returns 404 JSON for any request under APIPrefix.
//   - Renders TemplateName for everything else, with template data:
//     .Assets     — frontend.Assets (CSSPath, JSPath, IsDev)
//     .AppState   — whatever StateBuilder returns (nil if not configured)
//
// Template authors are responsible for injecting .AppState into the page.
// go-core does not mandate the variable name or the injection mechanism.
func RegisterSPA(engine *gin.Engine, cfg SPAConfig, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	cfg = cfg.withDefaults()

	// In production, resolve once — the dist files never change at runtime.
	// In dev, resolve per-request so Vite start/stop is detected automatically.
	var productionAssets *Assets
	if !cfg.DevMode {
		a := ResolveAssets(cfg.Vite)
		if !a.IsDev {
			productionAssets = &a
		}
	}

	log.Info("frontend: SPA registered with app state builder",
		"template", cfg.TemplateName,
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
			appState = cfg.StateBuilder(ctx)
		}

		nonce, _ := ctx.Get("nonce")
		log.Info("frontend: SPA rendering", "nonce", nonce)

		ctx.HTML(200, cfg.TemplateName, gin.H{
			"assets":   assets,
			"appState": appState,
			"Nonce":    nonce,
		})
	})
}

// RegisterSPAWithTemplates is like RegisterSPA but also sets the template FuncMap
// and loads templates from the given glob. Use this if you want go-core
// to own the full template setup. If you manage templates yourself,
// call RegisterSPA directly.
func RegisterSPAWithTemplates(engine *gin.Engine, cfg SPAConfig, log *slog.Logger) {
	cfg = cfg.withDefaults()

	funcs := BuiltinFuncMap()
	for k, v := range cfg.ExtraFuncs {
		funcs[k] = v
	}
	engine.SetFuncMap(funcs)
	engine.LoadHTMLGlob(cfg.TemplatesPath)

	RegisterSPA(engine, cfg, log)
}

// AppStateBuilder is a function the application provides.
// It receives the current request context and returns any value
// that can be serialised to JSON. The result is injected into
// the HTML template as window.APP_STATE.
//
// Keep it cheap: this runs on every non-API page load.
type AppStateBuilder func(ctx *gin.Context) any
