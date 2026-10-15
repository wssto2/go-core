package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/datatable"
)

// Response is the standard envelope for successful API responses.
type Response struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Meta    any    `json:"meta,omitempty"`
	Message string `json:"message,omitempty"`
}

// JSON sends a standardized success response.
func JSON(ctx *gin.Context, code int, data any, meta ...any) {
	resp := Response{
		Success: true,
		Data:    data,
	}
	if len(meta) > 0 {
		resp.Meta = meta[0]
	}
	ctx.JSON(code, resp)
}

// Created sends a 201 Created standardized response.
func Created(ctx *gin.Context, data any) {
	JSON(ctx, http.StatusCreated, data)
}

// WriteAccepted sends a 202 Accepted standardized response, for an endpoint that queues background work and answers with
// a receipt rather than the created resource.
func WriteAccepted(ctx *gin.Context, data any) {
	JSON(ctx, http.StatusAccepted, data)
}

// NoContent sends a 204 No Content response.
func NoContent(ctx *gin.Context) {
	ctx.Status(http.StatusNoContent)
}

// PageMeta is the meta of a page: the paging numbers with the enrichment data of result.Meta merged in.
func PageMeta[T any](result *datatable.DatatableResult[T]) gin.H {
	meta := gin.H{
		"total":     result.Total,
		"page":      result.Page,
		"per_page":  result.PerPage,
		"last_page": result.LastPage,
		"from":      result.From,
		"to":        result.To,
	}
	for k, v := range result.Meta {
		meta[k] = v
	}

	return meta
}

// Paginated sends a standard 200 with DatatableResult pagination meta pre-filled.
// Any enrichment data stored in result.Meta (e.g. from WithAuthors) is merged
// into the pagination meta so callers never need to do this manually.
func Paginated[T any](ctx *gin.Context, result *datatable.DatatableResult[T]) {
	JSON(ctx, http.StatusOK, result.Data, PageMeta(result))
}

// AutocompleteOption represents a standard option for autocomplete fields.
type AutocompleteOption struct {
	Label       string         `json:"label"`
	Value       any            `json:"value"`
	Description string         `json:"description,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
}
