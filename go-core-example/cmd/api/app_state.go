package main

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"go-core-example/internal/domain/product"

	"github.com/gin-gonic/gin"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/bootstrap"
	corei18n "github.com/wssto2/go-core/i18n"
)

// pageHeadData is rendered into the HTML <head>.
// This is where translated SEO metadata belongs.
type pageHeadData struct {
	Title           string `json:"title"`
	MetaDescription string `json:"metaDescription"`
	MetaKeywords    string `json:"metaKeywords"`
}

// pageBootstrapState is serialized into window.APP_STATE for the Vue app.
// Only put data here that the client needs immediately at boot.
type pageBootstrapState struct {
	AppName     string                       `json:"appName"`
	Env         string                       `json:"env"`
	Locale      string                       `json:"locale"`
	Path        string                       `json:"path"`
	APIBase     string                       `json:"apiBase"`
	Viewer      *viewerBootstrapState        `json:"viewer,omitempty"`
	ViewerError string                       `json:"viewerError,omitempty"`
	Catalog     catalogBootstrapState        `json:"catalog"`
	Product     *productDetailBootstrapState `json:"product,omitempty"`
}

// pageShellData is the full server-rendered model for the SPA shell.
// It intentionally separates:
//   - Head      -> document metadata rendered by the server
//   - Bootstrap -> client state serialized for Vue hydration/boot
type pageShellData struct {
	Head      pageHeadData       `json:"head"`
	Bootstrap pageBootstrapState `json:"bootstrap"`
}

type viewerBootstrapState struct {
	ID       int      `json:"id"`
	Username string   `json:"username"`
	Policies []string `json:"policies"`
}

type catalogBootstrapState struct {
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Total       int                  `json:"total"`
	Active      int                  `json:"active"`
	LowStock    int                  `json:"lowStock"`
	Error       string               `json:"error,omitempty"`
	Products    []catalogProductCard `json:"products,omitempty"`
}

type catalogProductCard struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	SKU         string  `json:"sku"`
	Description string  `json:"description,omitempty"`
	Price       float64 `json:"price"`
	Stock       int     `json:"stock"`
	Active      bool    `json:"active"`
}

type productDetailBootstrapState struct {
	ID           int     `json:"id,omitempty"`
	Name         string  `json:"name,omitempty"`
	SKU          string  `json:"sku,omitempty"`
	Description  string  `json:"description,omitempty"`
	Price        float64 `json:"price,omitempty"`
	Stock        int     `json:"stock,omitempty"`
	Active       bool    `json:"active"`
	ImageURL     string  `json:"imageUrl,omitempty"`
	ThumbnailURL string  `json:"thumbnailUrl,omitempty"`
	ImageStatus  string  `json:"imageStatus,omitempty"`
	Error        string  `json:"error,omitempty"`
}

type viewerBootstrapIdentity interface {
	coreauth.Identifiable
	GetEmail() string
	GetPolicies() []string
}

type pageTranslator interface {
	T(key, lang string) string
	TWith(key, lang string, params map[string]any) string
}

type catalogProductListFunc func(ctx context.Context) ([]product.Product, error)
type catalogProductGetFunc func(ctx context.Context, id int) (product.Product, error)

// pageShellRequest is the transport-agnostic input to the page composer.
// Keeping Gin out of this layer makes the composition logic easier to test and
// easier to reuse if the transport changes later.
type pageShellRequest struct {
	Path                string
	Locale              string
	AuthorizationHeader string
}

// pageShellComposer owns "which head + bootstrap data should this page get?".
// For multi-page apps, keep this interface and either:
//   - create one composer per page, plus a small router/dispatcher, or
//   - make one composer switch on req.Path internally.
type pageShellComposer interface {
	ComposePageShell(ctx context.Context, req pageShellRequest) (pageShellData, error)
}

// catalogPageShellComposer is the example's page-specific composition layer.
// It assembles translated document metadata plus the bootstrap state needed for
// both the catalog listing and the product detail route.
type catalogPageShellComposer struct {
	appName             string
	env                 string
	apiBase             string
	defaultLocale       string
	translator          pageTranslator
	tokenCfg            coreauth.TokenConfig
	viewerResolver      coreauth.IdentityResolver
	listCatalogProducts catalogProductListFunc
	getCatalogProduct   catalogProductGetFunc
}

func newCatalogPageShellComposer(
	cfg bootstrap.Config,
	translator pageTranslator,
	tokenCfg coreauth.TokenConfig,
	viewerResolver coreauth.IdentityResolver,
	listCatalogProducts catalogProductListFunc,
	getCatalogProduct catalogProductGetFunc,
) catalogPageShellComposer {
	defaultLocale := cfg.I18n.DefaultLocale
	if defaultLocale == "" {
		defaultLocale = "en"
	}

	return catalogPageShellComposer{
		appName:             cfg.App.Name,
		env:                 cfg.App.Env,
		apiBase:             "/api/v1",
		defaultLocale:       defaultLocale,
		translator:          translator,
		tokenCfg:            tokenCfg,
		viewerResolver:      viewerResolver,
		listCatalogProducts: listCatalogProducts,
		getCatalogProduct:   getCatalogProduct,
	}
}

func (c catalogPageShellComposer) ComposePageShell(ctx context.Context, req pageShellRequest) (pageShellData, error) {
	locale := normalizeLocale(req.Locale, c.defaultLocale)
	shell := pageShellData{
		Bootstrap: pageBootstrapState{
			AppName: c.appName,
			Env:     c.env,
			Locale:  locale,
			Path:    req.Path,
			APIBase: c.apiBase,
		},
	}

	viewer, err := c.resolveViewer(ctx, req.AuthorizationHeader)
	if err != nil {
		shell.Bootstrap.ViewerError = classifyViewerError(err)
	} else if viewer != nil {
		shell.Bootstrap.Viewer = viewer
	}

	shell.Bootstrap.Catalog = c.composeCatalog(ctx, locale, req.Path)
	if productID, ok := parseProductDetailPath(req.Path); ok {
		shell.Bootstrap.Product = c.composeProductDetail(ctx, productID)
	}

	shell.Head = c.composeHead(locale, req.Path, shell.Bootstrap.Product)
	return shell, nil
}

func (c catalogPageShellComposer) composeHead(locale, path string, productDetail *productDetailBootstrapState) pageHeadData {
	if productDetail != nil {
		if productDetail.Error == "" && productDetail.ID > 0 {
			params := map[string]any{
				"name":  productDetail.Name,
				"sku":   productDetail.SKU,
				"stock": productDetail.Stock,
			}
			return pageHeadData{
				Title:           c.translateWith(locale, "seo.product.title", params),
				MetaDescription: c.translateWith(locale, "seo.product.description", params),
				MetaKeywords:    c.translateWith(locale, "seo.product.keywords", params),
			}
		}

		return pageHeadData{
			Title:           c.translate(locale, "seo.productMissing.title"),
			MetaDescription: c.translate(locale, "seo.productMissing.description"),
			MetaKeywords:    c.translate(locale, "seo.productMissing.keywords"),
		}
	}

	switch path {
	case "/products":
		return pageHeadData{
			Title:           c.translate(locale, "seo.catalog.title"),
			MetaDescription: c.translate(locale, "seo.catalog.description"),
			MetaKeywords:    c.translate(locale, "seo.catalog.keywords"),
		}
	default:
		return pageHeadData{
			Title:           c.translate(locale, "seo.home.title"),
			MetaDescription: c.translate(locale, "seo.home.description"),
			MetaKeywords:    c.translate(locale, "seo.home.keywords"),
		}
	}
}

func (c catalogPageShellComposer) composeCatalog(ctx context.Context, locale, path string) catalogBootstrapState {
	descriptionKey := "catalog.description.home"
	includeProductCards := true
	switch {
	case path == "/products":
		descriptionKey = "catalog.description.index"
	case isProductDetailPath(path):
		descriptionKey = "catalog.description.detail"
		includeProductCards = false
	}

	catalog := catalogBootstrapState{
		Title:       c.translate(locale, "catalog.title"),
		Description: c.translate(locale, descriptionKey),
	}

	if c.listCatalogProducts == nil {
		catalog.Error = c.translate(locale, "catalog.error.unavailable")
		return catalog
	}

	products, err := c.listCatalogProducts(ctx)
	if err != nil {
		catalog.Error = c.translate(locale, "catalog.error.load")
		return catalog
	}

	for _, p := range products {
		card := catalogProductCard{
			ID:          p.ID,
			Name:        p.Name,
			SKU:         p.SKU,
			Description: p.Description.Get(),
			Price:       p.Price.Get(),
			Stock:       p.Stock,
			Active:      p.Active.Get(),
		}
		catalog.Total++
		if card.Active {
			catalog.Active++
		}
		if card.Stock > 0 && card.Stock <= 10 {
			catalog.LowStock++
		}
		if includeProductCards {
			catalog.Products = append(catalog.Products, card)
		}
	}

	return catalog
}

func (c catalogPageShellComposer) composeProductDetail(ctx context.Context, productID int) *productDetailBootstrapState {
	if c.getCatalogProduct == nil {
		return &productDetailBootstrapState{Error: "product detail query is unavailable"}
	}

	p, err := c.getCatalogProduct(ctx, productID)
	if err != nil {
		return &productDetailBootstrapState{Error: "failed to load product details"}
	}

	return &productDetailBootstrapState{
		ID:           p.ID,
		Name:         p.Name,
		SKU:          p.SKU,
		Description:  p.Description.Get(),
		Price:        p.Price.Get(),
		Stock:        p.Stock,
		Active:       p.Active.Get(),
		ImageURL:     p.ImageURL.Get(),
		ThumbnailURL: p.ThumbnailURL.Get(),
		ImageStatus:  p.ImageStatus.Get(),
	}
}

// spaShellDataBuilder is only the adapter required by go-core's current
// WithSPA(...) API. It converts the incoming Gin request into pageShellRequest
// and delegates all real page decisions to the composer.
type spaShellDataBuilder struct {
	composer pageShellComposer
}

func newSPAShellDataBuilder(composer pageShellComposer) spaShellDataBuilder {
	return spaShellDataBuilder{composer: composer}
}

func (b spaShellDataBuilder) Build(ctx *gin.Context) any {
	shell, err := b.composer.ComposePageShell(ctx.Request.Context(), newPageShellRequest(ctx))
	if err != nil {
		locale := normalizeLocale(requestedLocale(ctx), "en")
		return pageShellData{
			Head: pageHeadData{
				Title:           "go-core example",
				MetaDescription: "Vue 3 + Vite SPA powered by go-core.",
				MetaKeywords:    "go-core, vue, vite, ssr, product catalog",
			},
			Bootstrap: pageBootstrapState{
				Locale:      locale,
				Path:        ctx.Request.URL.Path,
				ViewerError: "failed to compose page shell",
			},
		}
	}
	return shell
}

func newPageShellRequest(ctx *gin.Context) pageShellRequest {
	return pageShellRequest{
		Path:                ctx.Request.URL.Path,
		Locale:              requestedLocale(ctx),
		AuthorizationHeader: ctx.GetHeader("Authorization"),
	}
}

func (c catalogPageShellComposer) resolveViewer(ctx context.Context, authHeader string) (*viewerBootstrapState, error) {
	if c.viewerResolver == nil {
		return nil, nil
	}

	token := extractBearerToken(authHeader)
	if token == "" {
		return nil, nil
	}

	claims, err := coreauth.ParseToken(token, c.tokenCfg)
	if err != nil {
		return nil, err
	}

	subject := claims.Subject
	if subject == "" && claims.UserID != 0 {
		subject = strconv.Itoa(claims.UserID)
	}
	if subject == "" {
		return nil, coreauth.ErrInvalidClaims
	}

	identity, err := c.viewerResolver(ctx, subject)
	if err != nil {
		return nil, err
	}

	viewer := &viewerBootstrapState{ID: identity.GetID()}
	if detailed, ok := identity.(viewerBootstrapIdentity); ok {
		viewer.Username = detailed.GetEmail()
		viewer.Policies = append([]string(nil), detailed.GetPolicies()...)
	}

	return viewer, nil
}

func (c catalogPageShellComposer) translate(locale, key string) string {
	if c.translator == nil {
		return key
	}
	return c.translator.T(key, locale)
}

func (c catalogPageShellComposer) translateWith(locale, key string, params map[string]any) string {
	if c.translator == nil {
		return key
	}
	return c.translator.TWith(key, locale, params)
}

func requestedLocale(ctx *gin.Context) string {
	if lang := strings.TrimSpace(ctx.Query("lang")); lang != "" {
		return lang
	}
	return ctx.GetHeader("Accept-Language")
}

func normalizeLocale(raw, fallback string) string {
	if fallback == "" {
		fallback = "en"
	}
	if raw == "" {
		return fallback
	}

	locale := strings.TrimSpace(strings.Split(raw, ",")[0])
	locale = strings.TrimSpace(strings.Split(locale, ";")[0])
	locale = strings.ToLower(locale)
	locale = strings.ReplaceAll(locale, "_", "-")
	if base, _, ok := strings.Cut(locale, "-"); ok && base != "" {
		locale = base
	}
	if locale == "" {
		return fallback
	}
	return locale
}

func isProductDetailPath(path string) bool {
	_, ok := parseProductDetailPath(path)
	return ok
}

func parseProductDetailPath(path string) (int, bool) {
	if !strings.HasPrefix(path, "/products/") {
		return 0, false
	}

	idPart := strings.TrimPrefix(path, "/products/")
	if idPart == "" || strings.Contains(idPart, "/") {
		return 0, false
	}

	id, err := strconv.Atoi(idPart)
	if err != nil || id <= 0 {
		return 0, false
	}

	return id, true
}

func extractBearerToken(header string) string {
	if header == "" || !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func classifyViewerError(err error) string {
	switch {
	case errors.Is(err, coreauth.ErrExpiredToken):
		return "viewer token has expired"
	case errors.Is(err, coreauth.ErrInvalidToken), errors.Is(err, coreauth.ErrInvalidClaims):
		return "viewer token is invalid"
	default:
		return "failed to resolve viewer"
	}
}

var _ pageTranslator = (*corei18n.Translator)(nil)
