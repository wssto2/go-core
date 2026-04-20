package datatable

import (
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
view := ctx.DefaultQuery("view", "")
page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
search := ctx.DefaultQuery("search", "")

// Accept both canonical and legacy param names.
perPageStr := ctx.DefaultQuery("per_page", ctx.DefaultQuery("length", ""))
perPage, _ := strconv.Atoi(perPageStr)

orderCol := ctx.DefaultQuery("order_col", ctx.DefaultQuery("order_column", ""))
orderDir := ctx.DefaultQuery("order_dir", ctx.DefaultQuery("order_direction", ""))

// Reserved keys — both canonical and legacy aliases — must not bleed into filters.
reserved := map[string]bool{
"page": true, "search": true, "view": true,
// canonical
"per_page": true, "order_col": true, "order_dir": true,
// legacy aliases
"length": true, "order_column": true, "order_direction": true,
}

filters := make(map[string]string)
for key, values := range ctx.Request.URL.Query() {
if !reserved[key] && len(values) > 0 {
filters[key] = values[0]
}
}

return QueryParams{
View:     view,
Page:     page,
PerPage:  perPage,
Search:   search,
OrderCol: orderCol,
OrderDir: orderDir,
Filters:  filters,
}
}
