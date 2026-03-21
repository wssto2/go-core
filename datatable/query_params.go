package datatable

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

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
		PerPage:  10,
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
	if p.PerPage < 1 || p.PerPage > 500 {
		return 10
	}
	return p.PerPage
}

func ParamsFromGin(ctx *gin.Context) QueryParams {
	view := ctx.DefaultQuery("view", "")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(ctx.DefaultQuery("per_page", "10"))
	search := ctx.DefaultQuery("search", "")
	orderCol := ctx.DefaultQuery("order_col", "")
	orderDir := ctx.DefaultQuery("order_dir", "")
	filters := make(map[string]string)

	reserved := map[string]bool{
		"page": true, "per_page": true, "search": true,
		"order_col": true, "order_dir": true,
	}

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
