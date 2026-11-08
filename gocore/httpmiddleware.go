package gocore

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// UseHTTP is Use for net/http middleware: each runs, in the order given and in line with the ones added by Use, before
// every route. A middleware that does not call the next handler ends the request; one that wraps the response writer or
// replaces the request (r.WithContext) is seen by everything after it. Prefer it to Use; Use stays for gin code.
func (a *App) UseHTTP(middleware ...func(http.Handler) http.Handler) {
	for _, mw := range middleware {
		a.middleware = append(a.middleware, ginOf(mw))
	}
}

// ginOf runs mw as a gin handler: what comes after it in the chain is the handler mw is given.
func ginOf(mw func(http.Handler) http.Handler) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		orig, called := ctx.Writer, false

		mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			ctx.Request = r

			if w != http.ResponseWriter(orig) {
				ctx.Writer = &wrappedWriter{ResponseWriter: orig, out: w}
			}

			ctx.Next()
		})).ServeHTTP(orig, ctx.Request)

		ctx.Writer = orig

		if !called {
			ctx.Abort()
		}
	}
}

// wrappedWriter sends what the rest of the chain writes through the writer a middleware wrapped, which in the end
// writes to the gin one (so its status and size stay right).
type wrappedWriter struct {
	gin.ResponseWriter
	out http.ResponseWriter
}

func (w *wrappedWriter) Header() http.Header { return w.out.Header() }

func (w *wrappedWriter) WriteHeader(code int) { w.out.WriteHeader(code) }

func (w *wrappedWriter) Write(p []byte) (int, error) { return w.out.Write(p) }

func (w *wrappedWriter) WriteString(s string) (int, error) { return w.out.Write([]byte(s)) }

func (w *wrappedWriter) Flush() {
	if f, ok := w.out.(http.Flusher); ok {
		f.Flush()

		return
	}

	w.ResponseWriter.Flush()
}
