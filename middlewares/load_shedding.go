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
//
// Use LoadShedding to protect CPU-bound or memory-bound route groups from
// overload. Unlike rate limiting (which measures request frequency per user),
// load shedding measures server-wide parallelism — once the server is already
// busy, new requests are rejected immediately rather than queued.
//
// A sensible default for maxConcurrent is runtime.NumCPU()*2 (the zero value
// triggers this default). Tune upward for I/O-bound workloads.
//
// Example — protect a compute-heavy route group:
//
//	api.Use(middlewares.LoadShedding(runtime.NumCPU()*4, 0))
//
// Note: for global DDoS/traffic-spike protection, prefer infrastructure-level
// solutions (CDN, WAF, load-balancer limits) over application-level load shedding.
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
			ctx.AbortWithStatusJSON(statusCode, gin.H{
				"success": false,
				"error":   "service overloaded",
			})
			return
		}
		defer atomic.AddInt32(&inFlight, -1)
		ctx.Next()
	}
}
