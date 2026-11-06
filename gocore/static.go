package gocore

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// sourceMapsHidden serves a directory without its source maps: a build's *.map files carry the original source,
// which a release keeps for reading a minified stack but never hands to a browser.
type sourceMapsHidden struct{ files http.FileSystem }

func (h sourceMapsHidden) Open(name string) (http.File, error) {
	if strings.HasSuffix(name, ".map") {
		return nil, fs.ErrNotExist
	}

	return h.files.Open(name) //nolint:wrapcheck // http.FileSystem's own error, read by http.FileServer
}

// immutableCache is the Cache-Control of a file whose name carries the hash of its content.
const immutableCache = "public, max-age=31536000, immutable"

// serveStatic mounts the files at the URL prefix. A file that is not there (a directory, a source map) is a 404 of
// its own: gin's StaticFS would hand it to the NoRoute handler, and the SPA shell would answer a missing script with
// a page and 200.
// Files for which immutable(name) is true (name relative to the prefix, no leading slash) are sent with a year-long
// immutable Cache-Control; immutable may be nil.
func serveStatic(engine *gin.Engine, prefix string, files http.FileSystem, immutable func(name string) bool) {
	files = sourceMapsHidden{files: files}
	prefix = strings.TrimRight(prefix, "/")
	server := http.StripPrefix(prefix, http.FileServer(files))

	handle := func(ctx *gin.Context) {
		name := ctx.Param("filepath")

		file, err := files.Open(name)
		if err != nil {
			http.NotFound(ctx.Writer, ctx.Request)

			return
		}

		info, err := file.Stat()
		_ = file.Close()

		if err != nil || info.IsDir() {
			http.NotFound(ctx.Writer, ctx.Request)

			return
		}

		if immutable != nil && immutable(strings.TrimPrefix(name, "/")) {
			ctx.Header("Cache-Control", immutableCache)
		}

		server.ServeHTTP(ctx.Writer, ctx.Request)
	}

	pattern := prefix + "/*filepath"
	engine.GET(pattern, handle)
	engine.HEAD(pattern, handle)
}
