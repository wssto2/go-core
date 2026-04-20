package validation

import (
	"errors"
	"fmt"

	"github.com/wssto2/go-core/apperr"
)

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

func NewErrUnknownRule(name, field string) ErrUnknownRule {
	return ErrUnknownRule{Name: name, Field: field}
}

// ErrInvalidRuleConfig is returned when a validation tag references a known rule
// with malformed or unsupported arguments. This is a programmer/configuration error.
type ErrInvalidRuleConfig struct {
	Rule   string
	Field  string
	Reason string
}

func (e ErrInvalidRuleConfig) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("validator: invalid configuration for rule %q on field %q", e.Rule, e.Field)
	}
	return fmt.Sprintf("validator: invalid configuration for rule %q on field %q: %s", e.Rule, e.Field, e.Reason)
}

func NewErrInvalidRuleConfig(rule, field, reason string) ErrInvalidRuleConfig {
	return ErrInvalidRuleConfig{
		Rule:   rule,
		Field:  field,
		Reason: reason,
	}
}

type ValidationError struct {
	*apperr.AppError
	Failures    map[string][]Failure
	DebugFields map[string][]string
}

func (e *ValidationError) Unwrap() error {
	return e.AppError
}

func NewValidationError(msg string, fieldFailures map[string][]Failure, debugFields map[string][]string) *ValidationError {
	return &ValidationError{
		AppError: &apperr.AppError{
			Err:      errors.New("validation failed"),
			Code:     apperr.CodeValidationError,
			Message:  msg,
			LogLevel: apperr.LevelWarn,
		},
		Failures:    cloneFailuresMap(fieldFailures),
		DebugFields: cloneDebugFieldsMap(debugFields),
	}
}
