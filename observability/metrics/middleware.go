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

		status := ctx.Writer.Status()
		statusStr := strconv.Itoa(status)
		path := ctx.FullPath() // uses route pattern, not raw URL — avoids cardinality explosion
		method := ctx.Request.Method

		m.requestDuration.WithLabelValues(method, path).Observe(time.Since(start).Seconds())
		m.requestCount.WithLabelValues(method, path, statusStr).Inc()
		if status >= 500 {
			m.requestErrors.WithLabelValues(method, path, statusStr).Inc()
		}
	}
}
