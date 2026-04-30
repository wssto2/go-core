package middlewares

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
	"go.opentelemetry.io/otel/trace"
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
			"request_id", ctx.GetString("request_id"),
			"status", status,
			"method", ctx.Request.Method,
			"path", path,
			"query", query,
			"ip", ctx.ClientIP(),
			"latency", latency.String(),
			"user_agent", ctx.Request.UserAgent(),
		}

		if actor, ok := auth.GetIdentifiable(ctx); ok {
			attrs = append(attrs, "actor_id", actor.GetID())
		}

		if spanCtx := trace.SpanFromContext(ctx.Request.Context()).SpanContext(); spanCtx.IsValid() {
			attrs = append(attrs, "trace_id", spanCtx.TraceID().String())
		}

		if len(ctx.Errors) > 0 {
			// Include the clean error message (not the raw gin error chain).
			attrs = append(attrs, "error", ctx.Errors.Last().Err.Error())

			// If error_handler stored the origin, include it for traceability.
			if file, ok := ctx.Get("error_file"); ok {
				attrs = append(attrs, "error_file", file, "error_line", ctx.GetInt("error_line"))
			}

			if status >= 500 {
				log.ErrorContext(ctx, "request failed", attrs...)
			} else {
				log.WarnContext(ctx, "request failed", attrs...)
			}
		} else if status >= 500 {
			// Panic recovery clears ctx.Errors but the status is still 5xx.
			log.ErrorContext(ctx, "request failed", attrs...)
		} else {
			log.InfoContext(ctx, "request completed", attrs...)
		}
	}
}
