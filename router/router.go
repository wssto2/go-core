package router

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/middlewares"
)

type Config struct {
	Env           string // "production" or "development"
	TemplatesPath string // e.g. "templates/*.html"
	StaticPath    string // e.g. "static"
	StaticURL     string // e.g. "/static"
	Cors          middlewares.CorsConfig
}

// NewEngine creates a pre-configured Gin engine with core middlewares.
func NewEngine(cfg Config) *gin.Engine {
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// Core Middlewares
	r.Use(middlewares.RequestID())
	r.Use(middlewares.RequestLogger())
	r.Use(middlewares.PanicRecovery())
	r.Use(middlewares.ErrorHandler(cfg.Env != "production"))
	r.Use(middlewares.Security())
	r.Use(middlewares.Cors(cfg.Cors))

	if cfg.TemplatesPath != "" {
		r.LoadHTMLGlob(cfg.TemplatesPath)
	}

	if cfg.StaticPath != "" && cfg.StaticURL != "" {
		r.Static(cfg.StaticURL, cfg.StaticPath)
	}

	return r
}
