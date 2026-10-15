package web

import (
	"maps"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/pagination"
)

// reservedParams are the query keys that are not filters: the canonical names and the legacy aliases datatable accepts.
var reservedParams = map[string]bool{
	"page": true, "search": true, "view": true,
	"per_page": true, "order_col": true, "order_dir": true,
	"length": true, "order_column": true, "order_direction": true,
}

// PageFromValues builds the pagination.Page of a query string, such as the url.Values field (`query:"*"`) of a typed
// route's input. It reads the parameters datatable.ParamsFromValues reads (canonical names and legacy aliases) and keeps
// every other key as a filter.
func PageFromValues(query url.Values) pagination.Page {
	// get is gin's DefaultQuery: the first value of a key that is present, even empty, else the fallback.
	get := func(key, fallback string) string {
		if query.Has(key) {
			return query.Get(key)
		}

		return fallback
	}

	number, _ := strconv.Atoi(get("page", "1"))
	size, _ := strconv.Atoi(get("per_page", get("length", "")))

	filters := make(map[string]string)

	for key, values := range query {
		if !reservedParams[key] && len(values) > 0 {
			filters[key] = values[0]
		}
	}

	return pagination.Page{
		Number:   number,
		Size:     size,
		Search:   get("search", ""),
		OrderCol: get("order_col", get("order_column", "")),
		OrderDir: get("order_dir", get("order_direction", "")),
		View:     get("view", ""),
		Filters:  filters,
	}
}

// ViewMeta is the meta entry of a page's views: key and count per view, in the order they are offered.
func ViewMeta(views []pagination.ViewCount) []gin.H {
	out := make([]gin.H, len(views))
	for i, view := range views {
		out[i] = gin.H{"key": view.Key, "count": view.Count}
	}

	return out
}

// WritePage answers a pagination.PageResult with a 200 in the envelope Paginated writes: the rows as data and the paging
// numbers, the result's own Meta and its views' counts as meta.
func WritePage[T any](ctx *gin.Context, result *pagination.PageResult[T]) {
	meta := gin.H{
		"total":     result.Total,
		"page":      result.Page,
		"per_page":  result.PerPage,
		"last_page": result.LastPage,
		"from":      result.From,
		"to":        result.To,
	}
	maps.Copy(meta, result.Meta)

	if len(result.Views) > 0 {
		meta["views"] = ViewMeta(result.Views)
	}

	JSON(ctx, http.StatusOK, result.Items, meta)
}

// WriteResource answers a pagination.Resource with a 200: Data as the payload and Meta as the top-level meta. On err it
// fails the request as Handle does.
func WriteResource[T any](ctx *gin.Context, result pagination.Resource[T], err error) {
	if err != nil {
		Fail(ctx, err)

		return
	}

	JSON(ctx, http.StatusOK, result.Data, result.Meta)
}
