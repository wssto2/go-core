package product

import (
	"go-core-example/internal/domain/auth"
	"net/http"

	"github.com/gin-gonic/gin"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/resource"
	"github.com/wssto2/go-core/web"
	"gorm.io/gorm"
)

// handler holds all HTTP handlers for the product routes.
// Unexported -- constructed only by the product Module.
type handler struct {
	service Service
	db      *gorm.DB
}

func newHandler(svc Service, db *gorm.DB) *handler {
	return &handler{service: svc, db: db}
}

// registerRoutes attaches all product routes. Receives tokenConfig so it can
// apply policy middleware without importing the container.
func (h *handler) registerRoutes(rg *gin.RouterGroup) {
	rg.GET("", h.list)
	rg.GET("/:id", h.show)
	rg.POST("",
		coreauth.Authorized(coreauth.GeneratePolicy("products", "create")),
		middlewares.BindRequest[CreateProductRequest](),
		h.create,
	)
	rg.PUT("/:id",
		coreauth.Authorized(coreauth.GeneratePolicy("products", "update")),
		middlewares.BindRequest[UpdateProductRequest](),
		h.update,
	)
	rg.DELETE("/:id",
		coreauth.Authorized(coreauth.GeneratePolicy("products", "delete")),
		h.delete,
	)
}

// list returns a paginated, searchable, filterable product list.
// GET /products?page=1&per_page=20&search=widget&active=1
func (h *handler) list(ctx *gin.Context) {
	params := datatable.ParamsFromGin(ctx)

	result, err := datatable.New[Product](h.db, params).
		WithColumns([]string{"id", "name", "sku", "price", "stock", "active", "created_at"}).
		WithSearchableFields([]string{"name", "sku"}).
		WithDefaultOrder("id", "desc").
		WithFilter(datatable.NewFilter("active", func(q *gorm.DB, val, table string) *gorm.DB {
			return q.Where(table+".active = ?", val)
		})).
		WithQuery(func(q *gorm.DB, table string) *gorm.DB {
			return q.Where(table + ".deleted_at IS NULL")
		}).
		Get()
	if err != nil {
		_ = ctx.Error(err)
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

	resp, err := resource.New[Product](h.db).
		WithAuthorLoader("CreatedBy", "UpdatedBy", func(db *gorm.DB, ids []int) ([]any, error) {
			products, err := h.service.GetMany(ctx.Request.Context(), ids)
			if err != nil {
				return nil, err
			}
			var result []any
			for _, p := range products {
				result = append(result, p)
			}
			return result, nil
		}).
		WithCount("audit_logs", "entity_id", "entity_type = 'products'").
		WithQuery(func(q *gorm.DB, table string) *gorm.DB {
			return q.Where(table + ".deleted_at IS NULL")
		}).
		FindByID(id)
	if err != nil {
		_ = ctx.Error(err)
		return
	}

	web.JSON(ctx, http.StatusOK, resp.Data, resp.Meta)
}

// create handles POST /products
func (h *handler) create(ctx *gin.Context) {
	req := middlewares.MustGetRequest[CreateProductRequest](ctx)
	actor := coreauth.MustGetUser[auth.AppUserData](ctx)

	created, err := h.service.Create(ctx.Request.Context(), *req, actor)
	if err != nil {
		_ = ctx.Error(err)
		return
	}
	web.Created(ctx, created)
}

// update handles PUT /products/:id
func (h *handler) update(ctx *gin.Context) {
	id, ok := web.GetPathID(ctx)
	if !ok {
		return
	}

	req := middlewares.MustGetRequest[UpdateProductRequest](ctx)
	actor := coreauth.MustGetUser[auth.AppUserData](ctx)

	updated, err := h.service.Update(ctx.Request.Context(), id, *req, actor)
	if err != nil {
		_ = ctx.Error(err)
		return
	}
	web.JSON(ctx, http.StatusOK, updated)
}

// delete handles DELETE /products/:id
func (h *handler) delete(ctx *gin.Context) {
	id, ok := web.GetPathID(ctx)
	if !ok {
		return
	}

	actor := coreauth.MustGetUser[auth.AppUserData](ctx)

	if err := h.service.Delete(ctx.Request.Context(), id, actor); err != nil {
		_ = ctx.Error(err)
		return
	}
	web.NoContent(ctx)
}
