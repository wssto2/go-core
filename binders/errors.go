package binders

import (
	"fmt"
	"strings"
)

// ErrValidation is returned when one or more stateless validation rules fail.
//
// Fields maps form field name → human-readable error messages (safe for clients).
// DebugFields maps form field name → rule names that failed (for tests).
//
// The shape mirrors validator.ErrValidation so the error handler middleware
// can handle both with the same code path.
type ErrValidation struct {
	Fields      map[string][]string
	DebugFields map[string][]string
}

func (e ErrValidation) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for field, msgs := range e.Fields {
		parts = append(parts, field+": "+strings.Join(msgs, ", "))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// ErrBadRequest is returned when the request body cannot be read or parsed.
// This is distinct from ErrValidation — it means the request is structurally
// broken, not that field values failed business rules.
type ErrBadRequest struct {
	Message string
}

func (e ErrBadRequest) Error() string {
	return fmt.Sprintf("bad request: %s", e.Message)
}
