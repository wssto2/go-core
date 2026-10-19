package product

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/web"
	"github.com/wssto2/go-core/web/upload"
)

// Permissions of the product feature. Reading is open to every signed-in user.
const (
	PermCreate = "catalog.product:create"
	PermUpdate = "catalog.product:update"
	PermDelete = "catalog.product:delete"
)

// DefinePermissions adds the feature's permissions to the application's catalogue.
func DefinePermissions(catalogue *authz.Catalogue) {
	catalogue.MustDefine(PermCreate)
	catalogue.MustDefine(PermUpdate)
	catalogue.MustDefine(PermDelete)
}

// base is the feature's path under the API prefix.
const base = "/v1/products"

// The product routes, declared as values: the declaration can be read without running anything, and the handler
// bound to it is a plain function the compiler ties to its input and output.
var (
	List   = route.Get[ListRequest, route.Page[Product]](base).Name("products.list").Authenticated()
	Show   = route.Get[ProductPath, route.WithMeta[Product, map[string]any]](base + "/:id").Name("products.show").Authenticated()
	Create = route.Post[CreateProductRequest, Product](base).Name("products.create").Requires(PermCreate).Status(http.StatusCreated)
	Update = route.Put[UpdateProductRequest, Product](base + "/:id").Name("products.update").Requires(PermUpdate)
	Delete = route.Delete[ProductPath, route.Empty](base + "/:id").Name("products.delete").Requires(PermDelete)
	// Image is a multipart upload, which the typed form cannot express.
	Image = route.Raw(http.MethodPost, base+"/:id/image").Name("products.image").Requires(PermUpdate)

	// Routes is the feature's contract: the frontend's route table is generated from it.
	Routes = route.Group("products", List, Show, Create, Update, Delete, Image)
)

// actor is the signed-in person as the service's auth.Identifiable.
type actor int

func (a actor) GetID() int { return int(a) }

// actorOf reads who acts from the authz principal the application's authentication put on the request.
func actorOf(ctx context.Context) (actor, error) {
	principal, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return 0, apperr.Unauthorized("user not authenticated")
	}

	return actor(principal.ID), nil
}

// handlers binds the routes to a service.
type handlers struct{ service Service }

// bind returns the bound routes of the feature.
func bind(service Service) []route.Handled {
	h := handlers{service: service}

	return []route.Handled{
		List.To(h.list),
		Show.To(h.show),
		Create.To(h.create),
		Update.To(h.update),
		Delete.To(h.delete),
		Image.To(h.uploadImage),
	}
}

// list returns a paginated, searchable, filterable product list.
// GET /products?page=1&per_page=20&search=widget&active=1
func (h handlers) list(ctx context.Context, in ListRequest) (route.Page[Product], error) {
	result, err := h.service.GetDatatable(ctx, datatable.ParamsFromValues(in.Filters))
	if err != nil {
		return route.Page[Product]{}, err
	}

	return route.Page[Product]{Result: *result}, nil
}

// show returns a single product with author info and sub-counts in the meta.
func (h handlers) show(ctx context.Context, in ProductPath) (route.WithMeta[Product, map[string]any], error) {
	resp, err := h.service.GetResource(ctx, in.ID)
	if err != nil {
		return route.WithMeta[Product, map[string]any]{}, err
	}

	return route.WithMeta[Product, map[string]any]{Items: resp.Data, Info: resp.Meta}, nil
}

func (h handlers) create(ctx context.Context, in CreateProductRequest) (Product, error) {
	who, err := actorOf(ctx)
	if err != nil {
		return Product{}, err
	}

	created, err := h.service.Create(ctx, in.ToInput(), who)
	if err != nil {
		return Product{}, err
	}

	return *created, nil
}

func (h handlers) update(ctx context.Context, in UpdateProductRequest) (Product, error) {
	who, err := actorOf(ctx)
	if err != nil {
		return Product{}, err
	}

	updated, err := h.service.Update(ctx, in.ID, in.ToInput(), who)
	if err != nil {
		return Product{}, err
	}

	return *updated, nil
}

func (h handlers) delete(ctx context.Context, in ProductPath) (route.Empty, error) {
	who, err := actorOf(ctx)
	if err != nil {
		return route.Empty{}, err
	}

	return route.Empty{}, h.service.Delete(ctx, in.ID, who)
}

// uploadImage handles POST /products/:id/image: it replaces or sets the image of an existing product.
// It stores the original from the multipart field "image" and answers 202 Accepted at once; the thumbnail and
// medium variants are made in the background by the image consumer.
func (h handlers) uploadImage(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		web.Fail(c, apperr.BadRequest("invalid id"))
		return
	}

	who, err := actorOf(c.Request.Context())
	if err != nil {
		web.Fail(c, err)
		return
	}

	f, err := upload.ValidateFile(c, "image", upload.Config{MaxSize: 10, IsPhoto: true})
	if err != nil {
		web.Fail(c, apperr.BadRequest("image field is required or invalid: "+err.Error()))
		return
	}

	defer func() { _ = f.File.Close() }()

	product, err := h.service.UploadImage(c.Request.Context(), id, f.File, f.Size, f.MimeType, who)
	if err != nil {
		web.Fail(c, err)
		return
	}

	web.JSON(c, http.StatusAccepted, product, nil)
}
