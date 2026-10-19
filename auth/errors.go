package auth

import "errors"

var (
	// ErrUnauthorized is what an authentication step answers for a missing or unacceptable credential.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden is what an authorization step answers for a caller who may not.
	ErrForbidden = errors.New("forbidden: insufficient permissions")
)
