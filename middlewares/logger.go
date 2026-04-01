package middlewares

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger logs incoming requests using the core logger.
func RequestLogger(log *slog.Logger) gin.HandlerFunc {
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
			log.ErrorContext(ctx, "request failed", attrs...)
		} else {
			log.InfoContext(ctx, "request completed", attrs...)
		}
	}
}
