package validation

import (
	"errors"

	"github.com/wssto2/go-core/apperr"
)

// Validatable is implemented by any input struct that can check its own rules.
type Validatable interface {
	Validate() error
}

// ValidateInput runs struct-tag-based validation on v.
// On validation failure it returns a *ValidationError which callers can
// inspect using errors.As to access per-field Failures.
// On any other error it returns apperr.BadRequest.
func ValidateInput(v any) error {
	err := New().Validate(v)
	if err == nil {
		return nil
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve
	}
	return apperr.BadRequest(err.Error())
}
