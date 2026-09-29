package access

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzhttp"
	"github.com/wssto2/go-core/web"
)

type renameRequest struct {
	Title string `json:"title"`
}

// RegisterRoutes mounts the lead routes and /me/access on a group that has
// already authenticated the user and run authzhttp.Principals.
//
// authzhttp.Require is the coarse gate ("holds it somewhere"); the service
// decides which leads.
func RegisterRoutes(rg *gin.RouterGroup, svc *Service, engine *authz.Engine) {
	rg.GET("/me/access", authzhttp.MeAccess(engine))

	leads := rg.Group("/leads")
	leads.GET("", authzhttp.Require(engine, LeadView), func(ctx *gin.Context) {
		leads, err := svc.List(ctx.Request.Context())
		web.Handle(ctx, leads, err)
	})
	leads.GET("/:id", authzhttp.Require(engine, LeadView), func(ctx *gin.Context) {
		id, ok := idParam(ctx)
		if !ok {
			return
		}
		lead, err := svc.Get(ctx.Request.Context(), id)
		web.Handle(ctx, lead, err)
	})
	leads.PUT("/:id", authzhttp.Require(engine, LeadUpdate), func(ctx *gin.Context) {
		id, ok := idParam(ctx)
		if !ok {
			return
		}
		var body renameRequest
		if err := ctx.ShouldBindJSON(&body); err != nil || body.Title == "" {
			_ = ctx.Error(apperr.BadRequest("title is required"))
			return
		}
		lead, err := svc.Rename(ctx.Request.Context(), id, body.Title)
		web.Handle(ctx, lead, err)
	})
}

func idParam(ctx *gin.Context) (int, bool) {
	id, err := strconv.Atoi(ctx.Param("id"))
	if err != nil || id <= 0 {
		_ = ctx.Error(apperr.BadRequest("invalid id"))
		return 0, false
	}
	return id, true
}
