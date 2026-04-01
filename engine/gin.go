package engine

import (
	"github.com/wssto2/go-core/middlewares"
)

type Config struct {
	Env           string // "production" or "development"
	TemplatesPath string // e.g. "templates/*.html"
	StaticPath    string // e.g. "static"
	StaticURL     string // e.g. "/static"
	Cors          middlewares.CorsConfig
}

func (c Config) withDefaults() Config {
	if c.Env == "" {
		c.Env = "development"
	}
	if c.TemplatesPath == "" {
		c.TemplatesPath = "templates/*.html"
	}
	if c.StaticPath == "" {
		c.StaticPath = "static"
	}
	if c.StaticURL == "" {
		c.StaticURL = "/static"
	}
	if c.Cors.AllowOrigins == nil {
		c.Cors.AllowOrigins = []string{"*"}
	}
	if c.Cors.AllowMethods == nil {
		c.Cors.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}
	if c.Cors.AllowHeaders == nil {
		c.Cors.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	}

	return c
}
