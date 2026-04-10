package product

import (
	"net/http"

	"go-core-example/internal/domain/auth"

	"github.com/gin-gonic/gin"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/web"
	"github.com/wssto2/go-core/web/upload"
)

// handler holds all HTTP handlers for the product routes.
// Unexported -- constructed only by the product Module.
type handler struct {
	service          Service
	idempotencyStore *middlewares.IdempotencyStore
}

func newHandler(svc Service, idempotencyStore *middlewares.IdempotencyStore) *handler {
	return &handler{service: svc, idempotencyStore: idempotencyStore}
}

// registerRoutes attaches all product routes. Receives tokenConfig so it can
// apply policy middleware without importing the container.
func (h *handler) registerRoutes(rg *gin.RouterGroup) {
	rg.GET("", h.list)
	rg.GET("/:id", h.show)
	rg.POST("",
		// Idempotency prevents duplicate products when a client retries a
		// timed-out POST. Supply an Idempotency-Key header to activate it.
		middlewares.Idempotency(h.idempotencyStore),
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
	rg.POST("/:id/image",
		coreauth.Authorize(coreauth.GeneratePolicy("products", "update")),
		h.uploadImage,
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

// create handles POST /products.
// Accepts both application/json and multipart/form-data.
// When multipart, an optional "image" field triggers immediate image upload
// alongside product creation (saves the round-trip of a separate image request).
func (h *handler) create(ctx *gin.Context) {
	req := middlewares.MustGetRequest[CreateProductRequest](ctx)
	actor := coreauth.MustGetUser[auth.User](ctx)

	created, err := h.service.Create(ctx.Request.Context(), req.ToInput(), actor)
	if err != nil {
		web.HandleCreate(ctx, created, err)
		return
	}

	// Optional image — only present when the request is multipart/form-data.
	f, uploadErr := upload.ValidateFile(ctx, "image", upload.Config{
		MaxSize: 10,
		IsPhoto: true,
	})
	if uploadErr == nil {
		defer func() { _ = f.File.Close() }()
		created, err = h.service.UploadImage(ctx.Request.Context(), created.ID, f.File, f.Size, f.MimeType, actor)
	}

	web.HandleCreate(ctx, created, err)
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

// uploadImage handles POST /products/:id/image.
// Replaces or sets the image for an existing product.
// Accepts a multipart field named "image", stores the original, and returns
// 202 Accepted immediately. Background processing (thumbnail, medium) happens
// asynchronously via imageWorker.
func (h *handler) uploadImage(ctx *gin.Context) {
	id, ok := web.GetPathID(ctx)
	if !ok {
		return
	}

	actor := coreauth.MustGetUser[auth.User](ctx)

	f, err := upload.ValidateFile(ctx, "image", upload.Config{
		MaxSize: 10,
		IsPhoto: true,
	})
	if err != nil {
		web.Fail(ctx, apperr.BadRequest("image field is required or invalid: "+err.Error()))
		return
	}
	defer func() { _ = f.File.Close() }()

	product, err := h.service.UploadImage(ctx.Request.Context(), id, f.File, f.Size, f.MimeType, actor)
	if err != nil {
		web.Fail(ctx, err)
		return
	}

	web.JSON(ctx, http.StatusAccepted, product, nil)
}
