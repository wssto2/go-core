package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestInstrumentHTTP_NoLabelMismatchPanic confirms that InstrumentHTTP does not
// panic due to a Prometheus label count mismatch.
func TestInstrumentHTTP_NoLabelMismatchPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := NewMetrics(nil)

	r := gin.New()
	r.Use(InstrumentHTTP(m))
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})
	r.GET("/error", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	require.NotPanics(t, func() {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	})

	// Also verify 5xx path records without panic.
	require.NotPanics(t, func() {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/error", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
