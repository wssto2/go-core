package middlewares

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/logger"
)

// PanicRecovery handles panics and logs them using the core logger.
func PanicRecovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(ctx *gin.Context, recovered any) {

		log := logger.GetFromContext(ctx)
		log.ErrorContext(ctx, "panic recovered",
			"error", recovered,
			"stack", string(debug.Stack()),
		)
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "Internal server error",
		})
	})
}
