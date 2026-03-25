package validation

import (
	"sort"
	"strings"

	"github.com/wssto2/go-core/apperr"
)

// Validatable is implemented by any input struct that can check its own rules.
type Validatable interface {
	Validate() error
}

// ValidateInput runs the struct-tag validator against v and returns a
// plain apperr.BadRequest on failure — suitable for non-HTTP callers.
// The error message lists all failing fields in a single string.
func ValidateInput(v any) error {
	err := New().Validate(v)
	if err == nil {
		return nil
	}

	if vv, ok := err.(*ValidationError); ok {
		errs := make([]string, 0, len(vv.Failures))
		for field, fieldErrs := range vv.Failures {
			for _, failure := range fieldErrs {
				errs = append(errs, field+": "+string(failure.Code))
			}
		}
		sort.Strings(errs) // deterministic order
		return apperr.BadRequest(strings.Join(errs, "; "))
	}

	return apperr.BadRequest(err.Error())
}
