package web

import (
	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/validation"
	"strconv"
)

// PathInt reads a path parameter that must be a positive integer. Anything else (missing, not a number, zero or
// negative) is answered 422 in the standard validation envelope with the error on the parameter's name, as a
// route-bound input answers it (must_be_number, must_be_positive_number), and the request is aborted: the handler
// must return on false.
func PathInt(ctx *gin.Context, key string) (int, bool) {
	code := validation.CodeMustBeNumber

	n, err := strconv.Atoi(ctx.Param(key))
	if err == nil {
		if n > 0 {
			return n, true
		}

		code = validation.CodeMustBePositiveNumber
	}

	_ = ctx.Error(validation.NewValidationError("validation failed",
		map[string][]validation.Failure{key: {validation.Fail(code)}}, map[string][]string{key: {"path"}}))
	ctx.Abort()

	return 0, false
}

// GetParamInt reads a URL path parameter by name and returns (int, bool).
// Returns (0, false) if the parameter is missing or cannot be parsed as an integer.
//
// Deprecated: it writes nothing on false, so a handler that ignores the flag runs on 0 and answers 200. Use PathInt,
// which answers 422 itself.
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
	return GetPathIDNamed(ctx, "id")
}

// GetPathIDNamed is GetPathID for a path parameter under an arbitrary name
// (e.g. "customerID", "evaluationID"). It validates the parameter is a
// positive integer and writes a 400 Bad Request using the standard error
// envelope if it is missing, non-numeric or non-positive. Returns (id, true)
// on success, (0, false) after writing the error response — the handler must
// return immediately on false.
func GetPathIDNamed(ctx *gin.Context, key string) (int, bool) {
	id, ok := GetParamInt(ctx, key)
	if !ok || id <= 0 {
		_ = ctx.Error(apperr.BadRequest("invalid " + key + ": must be a positive integer"))
		ctx.Abort()
		return 0, false
	}
	return id, true
}
