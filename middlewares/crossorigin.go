package middlewares

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CrossOrigin refuses, with 403, a cross-origin browser request that changes state
// (an unsafe method whose Sec-Fetch-Site or Origin says another site sent it), as
// cop decides; safe methods and requests without browser headers pass. It guards
// cookie-authenticated routes against CSRF. Build cop with
// http.NewCrossOriginProtection and add its trusted origins or bypass patterns:
//
//	app.Use(middlewares.CrossOrigin(http.NewCrossOriginProtection()))
func CrossOrigin(cop *http.CrossOriginProtection) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := cop.Check(ctx.Request); err != nil {
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "error": "cross-origin request refused"})
			return
		}

		ctx.Next()
	}
}
