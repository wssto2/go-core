package web

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"strconv"
)

// GetParamInt reads a URL path parameter by name and returns (int, bool).
// Returns (0, false) if the parameter is missing or cannot be parsed as an integer.

func GetParamInt(ctx *gin.Context, key string) (int, bool) {
	val := ctx.Param(key)
	if val == "" {
		return 0, false
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 0, false
	}
	return i, true
}

// GetQueryInt reads a URL query parameter by name and returns (int, bool).
// Returns (0, false) if the parameter is missing or cannot be parsed as an integer.
func GetQueryInt(ctx *gin.Context, key string) (int, bool) {
	val := ctx.Query(key)
	if val == "" {
		return 0, false
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 0, false
	}
	return i, true
}

// GetPathID reads the :id URL path parameter, validates it is a positive integer,
// and writes a 400 Bad Request using the standard error envelope if not.
// Returns (id, true) on success, (0, false) after writing the error response.
// The handler must return immediately on false.
func GetPathID(ctx *gin.Context) (int, bool) {
	id, ok := GetParamInt(ctx, "id")
	if !ok || id <= 0 {
		_ = ctx.Error(apperr.BadRequest("invalid id: must be a positive integer"))
		ctx.Abort()
		return 0, false
	}
	return id, true
}
