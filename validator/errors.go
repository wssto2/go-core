package validator

import (
	"fmt"
	"strings"
)

// ErrValidation is returned when one or more validation rules fail.
// It carries both human-readable messages (Errors) and rule names (DebugErrors)
// so test helpers can assert on specific rules without string-matching messages.
type ErrValidation struct {
	// Errors maps form field name → human-readable error messages.
	// These are safe to return to API clients.
	Errors map[string][]string

	// DebugErrors maps form field name → rule names that failed.
	// Used in tests via FieldHasError. Never expose to end users in production.
	DebugErrors map[string][]string
}

func (e ErrValidation) Error() string {
	parts := make([]string, 0, len(e.Errors))
	for field, msgs := range e.Errors {
		parts = append(parts, field+": "+strings.Join(msgs, ", "))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// ErrUnknownRule is returned when a validation tag references a rule name
// that has not been registered. This is always a programming error.
type ErrUnknownRule struct {
	Name  string // rule name from the tag
	Field string // field where it was encountered
}

func (e ErrUnknownRule) Error() string {
	return fmt.Sprintf(
		"validator: unknown rule %q on field %q — did you forget to call validator.Register(%q, ...)?",
		e.Name, e.Field, e.Name,
	)
}

// ValidationResponse is the JSON shape returned to API clients on 422.
// Used by the error handler middleware to serialise ErrValidation.
type ValidationResponse struct {
	Message     string              `json:"message"`
	Errors      map[string][]string `json:"errors"`
	DebugErrors map[string][]string `json:"debug_errors,omitempty"` // omit in production
}
