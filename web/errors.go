package web

import (
	"net/http"

	"github.com/wssto2/go-core/apperr"
)

var codeToHTTP = map[apperr.Code]int{
	apperr.CodeInternal:         http.StatusInternalServerError,
	apperr.CodeNotFound:         http.StatusNotFound,
	apperr.CodeUnauthenticated:  http.StatusUnauthorized,
	apperr.CodePermissionDenied: http.StatusForbidden,
	apperr.CodeInvalidArgument:  http.StatusUnprocessableEntity,
	apperr.CodeAlreadyExists:    http.StatusConflict,
	apperr.CodeBadRequest:       http.StatusBadRequest,
	apperr.CodeValidationError:  http.StatusUnprocessableEntity,
}

// GetHTTPStatus extracts the status code from an error
func GetHTTPStatus(err error) int {
	if ae, ok := err.(*apperr.AppError); ok {
		if status, exists := codeToHTTP[ae.Code]; exists {
			return status
		}
	}
	return http.StatusInternalServerError
}
