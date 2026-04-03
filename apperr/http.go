package apperr

import (
	"errors"
	"net/http"
)

var codeToHTTP = map[Code]int{
	CodeInternal:         http.StatusInternalServerError,
	CodeNotFound:         http.StatusNotFound,
	CodeUnauthenticated:  http.StatusUnauthorized,
	CodePermissionDenied: http.StatusForbidden,
	CodeInvalidArgument:  http.StatusUnprocessableEntity,
	CodeAlreadyExists:    http.StatusConflict,
	CodeBadRequest:       http.StatusBadRequest,
	CodeValidationError:  http.StatusUnprocessableEntity,
}

// GetHTTPStatus extracts the HTTP status code from an error.
// It unwraps error chains, so errors wrapped with fmt.Errorf("%w", appErr) work correctly.
func GetHTTPStatus(err error) int {
	var ae *AppError
	if errors.As(err, &ae) {
		if status, exists := codeToHTTP[ae.Code]; exists {
			return status
		}
	}
	return http.StatusInternalServerError
}
