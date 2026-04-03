package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestFail_AbortsHandlerChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	var secondCalled bool
	r.GET("/", func(c *gin.Context) {
		Fail(c, errors.New("boom"))
	}, func(c *gin.Context) {
		secondCalled = true
		c.String(http.StatusOK, "should not reach here")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)

	assert.False(t, secondCalled, "handler after Fail must not be called")
	assert.Equal(t, 1, len(r.Routes()), "sanity check")
}
