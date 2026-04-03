package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func Handle[T any](ctx *gin.Context, result T, err error) {
	if err != nil {
		Fail(ctx, err)
		return
	}
	JSON(ctx, http.StatusOK, result)
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
