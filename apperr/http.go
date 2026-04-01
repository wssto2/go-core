package apperr

import "net/http"

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

// GetHTTPStatus extracts the status code from an error
func GetHTTPStatus(err error) int {
	if ae, ok := err.(*AppError); ok {
		if status, exists := codeToHTTP[ae.Code]; exists {
			return status
		}
	}
	return http.StatusInternalServerError
}
