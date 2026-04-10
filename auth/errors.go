package auth

import "errors"

var (
	ErrMissingToken  = errors.New("missing authorization token")
	ErrInvalidToken  = errors.New("invalid authorization token")
	ErrExpiredToken  = errors.New("token has expired")
	ErrInvalidClaims = errors.New("invalid token claims")
	ErrInvalidConfig = errors.New("invalid token configuration")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden: insufficient permissions")
)
