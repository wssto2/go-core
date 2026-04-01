package product

import (
	"go-core-example/internal/domain/auth"
	"net/http"

	"github.com/gin-gonic/gin"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/web"
)

// handler holds all HTTP handlers for the product routes.
// Unexported -- constructed only by the product Module.
type handler struct {
	service Service
}

func newHandler(svc Service) *handler {
	return &handler{service: svc}
}

// registerRoutes attaches all product routes. Receives tokenConfig so it can
// apply policy middleware without importing the container.
func (h *handler) registerRoutes(rg *gin.RouterGroup) {
	rg.GET("", h.list)
	rg.GET("/:id", h.show)
	rg.POST("",
		coreauth.Authorize(coreauth.GeneratePolicy("products", "create")),
		middlewares.BindRequest[CreateProductRequest](),
		h.create,
	)
	rg.PUT("/:id",
		coreauth.Authorize(coreauth.GeneratePolicy("products", "update")),
		middlewares.BindRequest[UpdateProductRequest](),
		h.update,
	)
	rg.DELETE("/:id",
		coreauth.Authorize(coreauth.GeneratePolicy("products", "delete")),
		h.delete,
	)
}

// list returns a paginated, searchable, filterable product list.
// GET /products?page=1&per_page=20&search=widget&active=1
func (h *handler) list(ctx *gin.Context) {
	params := datatable.ParamsFromGin(ctx)

	result, err := h.service.GetDatatable(ctx.Request.Context(), params)
	if err != nil {
		web.Fail(ctx, err)
		return
	}

	web.Paginated(ctx, result)
}

// show returns a single product with author info and sub-counts in Meta.
// GET /products/:id
func (h *handler) show(ctx *gin.Context) {
	id, ok := web.GetPathID(ctx)
	if !ok {
		return
	}

	resp, err := h.service.GetResource(ctx.Request.Context(), id)
	if err != nil {
		web.Fail(ctx, err)
		return
	}

	web.JSON(ctx, http.StatusOK, resp.Data, resp.Meta)
}

// create handles POST /products
func (h *handler) create(ctx *gin.Context) {
	req := middlewares.MustGetRequest[CreateProductRequest](ctx)
	actor := coreauth.MustGetUser[auth.User](ctx)

	opts := req.ToInput()

	created, err := h.service.Create(ctx.Request.Context(), opts, actor)

	web.Handle(ctx, created, err)
}

// update handles PUT /products/:id
func (h *handler) update(ctx *gin.Context) {
	id, ok := web.GetPathID(ctx)
	if !ok {
		return
	}

	req := middlewares.MustGetRequest[UpdateProductRequest](ctx)
	actor := coreauth.MustGetUser[auth.User](ctx)

	opts := req.ToInput()

	updated, err := h.service.Update(ctx.Request.Context(), id, opts, actor)

	web.Handle(ctx, updated, err)
}

// delete handles DELETE /products/:id
func (h *handler) delete(ctx *gin.Context) {
	id, ok := web.GetPathID(ctx)
	if !ok {
		return
	}

	actor := coreauth.MustGetUser[auth.User](ctx)

	if err := h.service.Delete(ctx.Request.Context(), id, actor); err != nil {
		web.Fail(ctx, err)
		return
	}
	web.NoContent(ctx)
}
