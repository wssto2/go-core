package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/middlewares"
)

func TestCrossOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cop := http.NewCrossOriginProtection()
	require.NoError(t, cop.AddTrustedOrigin("https://admin.example.com"))

	r := gin.New()
	r.Use(middlewares.CrossOrigin(cop))
	r.Any("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for name, tc := range map[string]struct {
		method string
		header map[string]string
		want   int
	}{
		"cross-site POST refused":   {http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		"foreign Origin refused":    {http.MethodDelete, map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		"same-origin POST passes":   {http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusNoContent},
		"trusted origin passes":     {http.MethodPost, map[string]string{"Origin": "https://admin.example.com"}, http.StatusNoContent},
		"no browser headers passes": {http.MethodPost, nil, http.StatusNoContent},
		"cross-site GET passes":     {http.MethodGet, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusNoContent},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", nil)
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, tc.want, rec.Code)
		})
	}
}
