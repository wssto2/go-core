package middlewares

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/wssto2/go-core/logger"
)

const HeaderXRequestID = "X-Request-ID"

// RequestID middleware ensures every request has a unique ID.
// It checks the X-Request-ID header; if missing, it generates a new UUID.
// The ID is then set in the response headers and the gin context.
func RequestID() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		requestID := ctx.GetHeader(HeaderXRequestID)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Set in header for the client to see
		ctx.Writer.Header().Set(HeaderXRequestID, requestID)

		// Set in gin context for handlers
		ctx.Set("request_id", requestID)

		// Inject into context.Context for the logger and downstream services
		ctx.Request = ctx.Request.WithContext(logger.WithRequestID(ctx.Request.Context(), requestID))

		ctx.Next()
	}
}

// GetRequestID retrieves the request ID from the gin context.
func GetRequestID(ctx *gin.Context) string {
	return ctx.GetString("request_id")
}
