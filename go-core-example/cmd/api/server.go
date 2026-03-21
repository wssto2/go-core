package main

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/router"
)

// buildEngine creates the pre-configured Gin engine with all core middleware:
// request ID, structured logger, panic recovery, error handler,
// security headers, and CORS.
func buildEngine(cfg AppConfig) *gin.Engine {
	return router.NewEngine(router.Config{
		Env: cfg.Env,
		Cors: middlewares.CorsConfig{
			AllowOrigins: cfg.CORSOrigins,
			AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowHeaders: []string{"Authorization", "Content-Type", "X-Request-ID"},
		},
	})
}
