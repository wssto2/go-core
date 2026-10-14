package datatable

import (
"net/url"
"strconv"

"github.com/gin-gonic/gin"
)

// defaultPerPage is the fallback page size when PerPage is zero or unset.
const defaultPerPage = 10

// maxPerPage is the upper bound a caller may request. Values outside
// [1, maxPerPage] silently clamp to defaultPerPage.
const maxPerPage = 500

type QueryParams struct {
View     string
Page     int
PerPage  int
Search   string
OrderCol string
OrderDir string
Filters  map[string]string
}

// DefaultParams returns sensible defaults.
func DefaultParams() QueryParams {
return QueryParams{
Page:     1,
PerPage:  defaultPerPage,
OrderCol: "id",
OrderDir: "asc",
Filters:  make(map[string]string),
}
}

func (p *QueryParams) GetPage() int {
if p.Page < 1 {
return 1
}
return p.Page
}

func (p *QueryParams) GetPerPage() int {
if p.PerPage < 1 || p.PerPage > maxPerPage {
return defaultPerPage
}
return p.PerPage
}

// ParamsFromGin extracts datatable query parameters from a Gin request.
//
// Canonical parameter names (preferred):
//
//page, per_page, search, order_col, order_dir, view
//
// Legacy aliases (accepted for backward compatibility with older frontends):
//
//length (= per_page), order_column (= order_col), order_direction (= order_dir)
//
// All other query parameters are treated as filter key-value pairs.
func ParamsFromGin(ctx *gin.Context) QueryParams {
	return ParamsFromValues(ctx.Request.URL.Query())
}

// ParamsFromValues is ParamsFromGin for a query already in hand, such as the url.Values field
// (query:"*") of a typed route's input: the same names, aliases and filters.
func ParamsFromValues(query url.Values) QueryParams {
	get := func(keys ...string) string {
		for _, key := range keys {
			if query.Has(key) {
				return query.Get(key)
			}
		}

		return ""
	}

	page := 1
	if query.Has("page") {
		page, _ = strconv.Atoi(query.Get("page"))
	}

	perPage, _ := strconv.Atoi(get("per_page", "length"))

	// Reserved keys, canonical and legacy, must not bleed into filters.
	reserved := map[string]bool{
		"page": true, "search": true, "view": true,
		"per_page": true, "order_col": true, "order_dir": true,
		"length": true, "order_column": true, "order_direction": true,
	}

	filters := make(map[string]string)

	for key, values := range query {
		if !reserved[key] && len(values) > 0 {
			filters[key] = values[0]
		}
	}

	return QueryParams{
		View:     get("view"),
		Page:     page,
		PerPage:  perPage,
		Search:   get("search"),
		OrderCol: get("order_col", "order_column"),
		OrderDir: get("order_dir", "order_direction"),
		Filters:  filters,
	}
}
