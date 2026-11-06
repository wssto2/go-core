package middlewares

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// Under runs middleware only for requests whose path is prefix or below it ("/admin" and "/admin/x", not
// "/administration"); every other request passes through untouched. Use it with App.Use for a guard that belongs to
// one part of the site:
//
//	app.Use(middlewares.Under("/admin", middlewares.CrossOrigin(http.NewCrossOriginProtection())))
func Under(prefix string, middleware gin.HandlerFunc) gin.HandlerFunc {
	base := strings.TrimRight(prefix, "/")

	return func(ctx *gin.Context) {
		path := ctx.Request.URL.Path
		if path == base || strings.HasPrefix(path, base+"/") {
			middleware(ctx)

			return
		}

		ctx.Next()
	}
}
