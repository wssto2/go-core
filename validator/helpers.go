package validator

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"

	"github.com/goccy/go-json"
)

// FieldHasError checks whether a specific validation rule failed for a specific
// field in an HTTP test response.
//
// Use this in integration tests to assert on rule names rather than
// message strings — message strings can change, rule names are stable.
//
// Example:
//
//	resp := performRequest(router, "POST", "/customers", body)
//	assert.True(t, validator.FieldHasError(resp, "email", "email"))
//	assert.True(t, validator.FieldHasError(resp, "vrsta", "in"))
func FieldHasError(response *httptest.ResponseRecorder, field string, rule string) bool {
	if response.Code != http.StatusUnprocessableEntity {
		return false
	}

	var body ValidationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		return false
	}

	if body.DebugErrors == nil {
		return false
	}

	return slices.Contains(body.DebugErrors[field], rule)
}

// validEmailRegexp is the canonical email validation regexp used across both
// the binder (stateless) and the validator (stateful) layers.
// Defined once here so both layers stay in sync.
var validEmailRegexp = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// IsValidEmail returns true if email is a syntactically valid email address.
//
// This replaces the previous broken implementation that used strings.Contains
// and strings.Index checks — those allowed values like "a@b." which are invalid.
//
// Empty string returns false. Use alongside a "required" check if the field
// is optional.
func IsValidEmail(email string) bool {
	if email == "" {
		return false
	}
	return validEmailRegexp.MatchString(email)
}
