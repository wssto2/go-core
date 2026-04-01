package middlewares

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// PanicRecovery handles panics and logs them using the core logger.
func PanicRecovery(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(ctx *gin.Context, recovered any) {
		log.ErrorContext(ctx, "panic recovered",
			"error", recovered,
			"stack", string(debug.Stack()),
		)
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "Internal server error",
		})
	})
}
