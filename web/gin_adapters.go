package web

import "github.com/gin-gonic/gin"

// Gin adapter for ValidationContext
type GinValidationContext struct {
	ctx *gin.Context
}

func NewGinValidationContext(ctx *gin.Context) GinValidationContext {
	return GinValidationContext{ctx: ctx}
}

func (g GinValidationContext) Locale() string {
	if l := g.ctx.GetString("locale"); l != "" {
		return l
	}

	return "en"
}
