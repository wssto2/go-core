package apperr

// Code represents a semantic error category
type Code string

const (
	CodeInternal         Code = "internal_error"
	CodeNotFound         Code = "not_found"
	CodeBadRequest       Code = "bad_request"
	CodeUnauthenticated  Code = "unauthenticated"
	CodePermissionDenied Code = "permission_denied"
	CodeInvalidArgument  Code = "invalid_argument"
	CodeAlreadyExists    Code = "already_exists"
	CodeValidationError  Code = "validation_error"
)
