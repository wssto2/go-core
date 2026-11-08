//nolint:paralleltest,testpackage,noctx,varnamelen,exhaustruct,exhaustruct_v5,wrapcheck,wsl_v5 // test style
package gocore

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ctxKey struct{}

type upperWriter struct{ http.ResponseWriter }

func (u upperWriter) Write(p []byte) (int, error) {
	return u.ResponseWriter.Write([]byte(strings.ToUpper(string(p))))
}

// upper does what a net/http middleware does: ends /stop, sets a header, wraps the writer, tags the request.
func upper(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stop" {
			w.WriteHeader(http.StatusTeapot)

			return
		}

		w.Header().Set("X-Seen", "yes")
		next.ServeHTTP(upperWriter{w}, r.WithContext(context.WithValue(r.Context(), ctxKey{}, "tagged")))
	})
}

func TestUseHTTP_RunsInLineWithTheChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	app := &App{}
	app.UseHTTP(upper)
	require.Len(t, app.middleware, 1)

	engine := gin.New()
	engine.Use(app.middleware...)
	engine.GET("/x", func(c *gin.Context) {
		c.String(http.StatusOK, "hello %v", c.Request.Context().Value(ctxKey{}))
	})
	engine.GET("/stop", func(*gin.Context) { t.Error("the middleware ended the request") })

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, "HELLO TAGGED", rec.Body.String(), "the wrapped writer and the new request reach the handler")
	assert.Equal(t, "yes", rec.Header().Get("X-Seen"))

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stop", nil))
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

func TestForwardRequest_HandsANetHTTPHandlerToTheUpstream(t *testing.T) {
	var path string

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		_, _ = io.WriteString(w, "legacy")
	})

	var builder *appBuilder

	app := fallthroughApp(t, h, func(b *appBuilder) { builder = b })
	forwarding := &App{forwardHTTP: builder.forwardHTTP, log: slog.New(slog.DiscardHandler)}

	app.engine.GET("/admin/compat", gin.WrapF(func(w http.ResponseWriter, r *http.Request) {
		forwarding.ForwardRequest(w, r, "/admin-legacy/compat")
	}))

	res := do(t, app, http.MethodGet, "/admin/compat", "")
	assert.Equal(t, "legacy", res.Body)
	assert.Equal(t, "/admin-legacy/compat", path)
	assert.Equal(t, []string{"SAMEORIGIN"}, res.Header.Values("X-Frame-Options"))
	assert.Empty(t, res.Header.Get("Content-Security-Policy"))
}

func TestForwardRequest_WithoutAFallthroughIsAnInternalError(t *testing.T) {
	rec := httptest.NewRecorder()
	(&App{log: slog.New(slog.DiscardHandler)}).ForwardRequest(rec, httptest.NewRequest(http.MethodGet, "/x", nil), "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestWithShell_SetsTheNetHTTPStateBuilder(t *testing.T) {
	app := &App{}
	WithShell(func(r *http.Request) (any, error) { return r.URL.Path, nil })(app)

	state, err := app.spa.shell(httptest.NewRequest(http.MethodGet, "/p", nil))
	require.NoError(t, err)
	assert.Equal(t, "/p", state)
}
