package middlewares

import (
	"net/http"
	"runtime"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// LoadShedding returns a Gin middleware that rejects requests when the
// number of concurrent in-flight requests exceeds maxConcurrent. If statusCode
// is 0, http.StatusServiceUnavailable (503) is used. The middleware maintains a
// per-middleware in-flight counter and ensures it's decremented even on panic.
func LoadShedding(maxConcurrent int, statusCode int) gin.HandlerFunc {
	if maxConcurrent <= 0 {
		maxConcurrent = runtime.NumCPU() * 2
	}
	if statusCode == 0 {
		statusCode = http.StatusServiceUnavailable
	}
	var inFlight int32
	return func(ctx *gin.Context) {
		cur := atomic.AddInt32(&inFlight, 1)
		if cur > int32(maxConcurrent) {
			// exceeded limit; decrement and reject immediately
			atomic.AddInt32(&inFlight, -1)
			ctx.AbortWithStatusJSON(statusCode, gin.H{"error": "service overloaded"})
			return
		}
		defer atomic.AddInt32(&inFlight, -1)
		ctx.Next()
	}
}
