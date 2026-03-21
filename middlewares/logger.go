package middlewares

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/logger"
)

// RequestLogger logs incoming requests using the core logger.
func RequestLogger() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()
		path := ctx.Request.URL.Path
		query := ctx.Request.URL.RawQuery

		ctx.Next()

		latency := time.Since(start)
		status := ctx.Writer.Status()

		attrs := []any{
			"status", status,
			"method", ctx.Request.Method,
			"path", path,
			"query", query,
			"ip", ctx.ClientIP(),
			"latency", latency.String(),
			"user_agent", ctx.Request.UserAgent(),
		}

		if len(ctx.Errors) > 0 {
			attrs = append(attrs, "error", ctx.Errors.String())
			logger.Log.ErrorContext(ctx, "request failed", attrs...)
		} else {
			logger.Log.InfoContext(ctx, "request completed", attrs...)
		}
	}
}
