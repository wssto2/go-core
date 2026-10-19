package web

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const originalBodyKey = "go-core.web.original_body"

// BodyLimit caps the body of every JSON (non-multipart) request at limit bytes: reading past it
// fails, and the error becomes a 413 where it is reported (the route binder, middlewares.ErrorHandler).
// A multipart upload is left to its own limit (web/upload). limit <= 0 sets none.
func BodyLimit(limit int64) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Set(originalBodyKey, ctx.Request.Body)
		capBody(ctx, limit)
	}
}

// BodyLimitFor replaces, for one route, the limit BodyLimit set: a route that takes a larger JSON body
// than the rest says so with Route.MaxBody.
func BodyLimitFor(limit int64) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if original, ok := ctx.Get(originalBodyKey); ok {
			if body, ok := original.(io.ReadCloser); ok {
				ctx.Request.Body = body
			}
		}

		capBody(ctx, limit)
	}
}

func capBody(ctx *gin.Context, limit int64) {
	if limit <= 0 || strings.HasPrefix(strings.ToLower(ctx.GetHeader("Content-Type")), "multipart/") {
		return
	}

	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, limit)
}
