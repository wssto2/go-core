package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/middlewares"
)

//nolint:paralleltest // gin.SetMode is global
func TestUnderRunsTheMiddlewareOnlyUnderThePrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(middlewares.Under("/admin/", func(ctx *gin.Context) {
		ctx.Header("X-Guarded", "yes")
		ctx.AbortWithStatus(http.StatusForbidden)
	}))

	for _, path := range []string{"/", "/api/x", "/administration", "/admin-legacy/x"} {
		engine.GET(path, func(ctx *gin.Context) { ctx.String(http.StatusOK, "ok") })
	}

	engine.GET("/admin", func(ctx *gin.Context) { ctx.String(http.StatusOK, "ok") })
	engine.GET("/admin/users", func(ctx *gin.Context) { ctx.String(http.StatusOK, "ok") })

	for path, want := range map[string]int{
		"/": 200, "/api/x": 200, "/administration": 200, "/admin-legacy/x": 200,
		"/admin": 403, "/admin/users": 403, "/admin/nope": 403,
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		assert.Equal(t, want, rec.Code, path)
		assert.Equal(t, want == 403, rec.Header().Get("X-Guarded") == "yes", path)
	}
}
