package web

import (
	"errors"
	"mime"
	"net/http"
	"path"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/storage"
)

// Attachment is the Content-Disposition that makes the browser save the file under name, for Serve.
func Attachment(name string) string { return Disposition("attachment", name) }

// Inline is the Content-Disposition that shows the file in the page and names it for a later save, for Serve.
func Inline(name string) string { return Disposition("inline", name) }

// Serve streams the file at key of disk as the response. A disk that can open a file for seeking (the local and memory
// ones) answers Range and If-Modified-Since and sets Last-Modified; any other is streamed whole. disposition is the
// Content-Disposition header, from Attachment or Inline, "" for none. A missing file answers the standard JSON 404, any
// other failure goes to the error handler, and the caller returns after Serve in both cases.
//
// Serve does not check who may read the file: call it from a route that has already done so. That is what keeps a
// private disk private.
//
//	web.Serve(c, documents, doc.Name, web.Attachment(doc.Name))
func Serve(ctx *gin.Context, disk storage.Driver, key, disposition string) {
	requestCtx := ctx.Request.Context()

	if o, ok := disk.(storage.Opener); ok {
		rs, info, err := o.Open(requestCtx, key)
		if err == nil {
			defer func() { _ = rs.Close() }() // best-effort: the response is already written

			setDisposition(ctx, disposition)
			http.ServeContent(ctx.Writer, ctx.Request, info.Name(), info.ModTime(), rs)

			return
		}

		if !errors.Is(err, errors.ErrUnsupported) {
			failServe(ctx, err)

			return
		}
	}

	body, err := disk.Get(requestCtx, key)
	if err != nil {
		failServe(ctx, err)

		return
	}
	defer func() { _ = body.Close() }() // best-effort: the response is already written

	contentType := mime.TypeByExtension(path.Ext(key))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	setDisposition(ctx, disposition)
	ctx.DataFromReader(http.StatusOK, -1, contentType, body, nil)
}

func setDisposition(ctx *gin.Context, disposition string) {
	if disposition != "" {
		ctx.Header("Content-Disposition", disposition)
	}
}

func failServe(ctx *gin.Context, err error) {
	if apperr.HasCode(err, apperr.CodeNotFound) {
		err = apperr.NotFound("file not found") // the key is the caller's business, not the client's
	}

	Fail(ctx, err)
}
