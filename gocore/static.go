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

// serveStatic mounts the directory at the URL prefix, answering 404 for its source maps.
func serveStatic(engine *gin.Engine, prefix, dir string) {
	engine.StaticFS(prefix, sourceMapsHidden{files: gin.Dir(dir, false)})
}
