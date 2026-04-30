package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/resource"
)

func Handle[T any](ctx *gin.Context, result T, err error) {
	if err != nil {
		Fail(ctx, err)
		return
	}
	JSON(ctx, http.StatusOK, result)
}

// HandleResource correctly unwraps a resource.Response, sending result.Data as
// the response payload and result.Meta as the top-level meta envelope.
// Use this instead of Handle when the service returns resource.Response[T].
func HandleResource[T any](ctx *gin.Context, result resource.Response[T], err error) {
	if err != nil {
		Fail(ctx, err)
		return
	}
	JSON(ctx, http.StatusOK, result.Data, result.Meta)
}

func HandleCreate[T any](ctx *gin.Context, result T, err error) {
	if err != nil {
		Fail(ctx, err)
		return
	}
	Created(ctx, result)
}

func Fail(ctx *gin.Context, err error) {
	_ = ctx.Error(err)
	ctx.Abort()
}
