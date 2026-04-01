package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// InstrumentHTTP returns a middleware that automatically records duration and
// status for every HTTP request. No manual calls needed anywhere.
func InstrumentHTTP(m *Metrics) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()
		ctx.Next()

		status := strconv.Itoa(ctx.Writer.Status())
		path := ctx.FullPath() // uses route pattern, not raw URL — avoids cardinality explosion
		method := ctx.Request.Method

		m.requestDuration.WithLabelValues(method, path, status).Observe(time.Since(start).Seconds())
		m.requestCount.WithLabelValues(method, path, status).Inc()
	}
}
