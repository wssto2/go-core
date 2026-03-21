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

// NoContent sends a 204 No Content response.
func NoContent(ctx *gin.Context) {
	ctx.Status(http.StatusNoContent)
}

// Paginated sends a standard 200 with DatatableResult pagination meta pre-filled.
func Paginated[T any](ctx *gin.Context, result *datatable.DatatableResult[T]) {
	JSON(ctx, http.StatusOK, result.Data, gin.H{
		"total":     result.Total,
		"page":      result.Page,
		"per_page":  result.PerPage,
		"last_page": result.LastPage,
		"from":      result.From,
		"to":        result.To,
	})
}

// AutocompleteOption represents a standard option for autocomplete fields.
type AutocompleteOption struct {
	Label       string         `json:"label"`
	Value       any            `json:"value"`
	Description string         `json:"description,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
}
