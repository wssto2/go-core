package web

import "github.com/gin-gonic/gin"

type HandlerFunc func(*gin.Context) error

// Wrap wraps a HandlerFunc into a gin.HandlerFunc.
func Wrap(h HandlerFunc) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := h(ctx); err != nil {
			_ = ctx.Error(err)
		}
	}
}
