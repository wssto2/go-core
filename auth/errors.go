package auth

import "errors"

var (
	ErrMissingToken  = errors.New("missing authorization token")
	ErrInvalidToken  = errors.New("invalid authorization token")
	ErrExpiredToken  = errors.New("token has expired")
	ErrInvalidClaims = errors.New("invalid token claims")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden: insufficient permissions")
)
