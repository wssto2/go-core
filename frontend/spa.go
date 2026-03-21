package frontend

import (
	"text/template"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"github.com/wssto2/go-core/logger"
)

// SPAConfig configures the SPA catch-all handler.
type SPAConfig struct {
	// Vite holds Vite dev server and asset resolution config.
	Vite ViteConfig

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
	ExtraFuncs template.FuncMap
}

func (c SPAConfig) withDefaults() SPAConfig {
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
func BuiltinFuncMap() template.FuncMap {
	return template.FuncMap{
		"toJSON": func(v any) string {
			b, _ := json.Marshal(v)
			return string(b)
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
func RegisterSPA(engine *gin.Engine, cfg SPAConfig) {
	cfg = cfg.withDefaults()

	assets := ResolveAssets(cfg.Vite)

	engine.NoRoute(func(ctx *gin.Context) {
		if len(ctx.Request.URL.Path) >= len(cfg.APIPrefix) &&
			ctx.Request.URL.Path[:len(cfg.APIPrefix)] == cfg.APIPrefix {
			ctx.JSON(404, gin.H{"error": "not found"})
			return
		}

		var appState any
		if cfg.StateBuilder != nil {
			appState = cfg.StateBuilder(ctx)
		}

		ctx.HTML(200, cfg.TemplateName, gin.H{
			"Assets":   assets,
			"AppState": appState,
		})
	})
}

// MustRegisterSPA is like RegisterSPA but also sets the template FuncMap
// and loads templates from the given glob. Use this if you want go-core
// to own the full template setup. If you manage templates yourself,
// call RegisterSPA directly.
func MustRegisterSPA(engine *gin.Engine, templateGlob string, cfg SPAConfig) {
	funcs := BuiltinFuncMap()
	for k, v := range cfg.ExtraFuncs {
		funcs[k] = v
	}
	engine.SetFuncMap(funcs)
	engine.LoadHTMLGlob(templateGlob)

	if cfg.StateBuilder != nil && logger.Log != nil {
		logger.Log.Info("frontend: SPA registered with app state builder",
			"template", cfg.TemplateName,
			"api_prefix", cfg.APIPrefix,
			"vite_dev", ResolveAssets(cfg.Vite).IsDev,
		)
	}

	RegisterSPA(engine, cfg)
}
