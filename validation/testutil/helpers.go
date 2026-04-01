package testutil

import (
	"net/http"
	"net/http/httptest"
	"slices"

	"github.com/goccy/go-json"
)

// FieldHasError checks whether a specific validation rule failed for a specific
// field in an HTTP test response.
func FieldHasError(response *httptest.ResponseRecorder, field string, rule string) bool {
	if response.Code != http.StatusUnprocessableEntity {
		return false
	}

	var body struct {
		DebugErrors map[string][]string `json:"debug_errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		return false
	}

	if body.DebugErrors == nil {
		return false
	}

	return slices.Contains(body.DebugErrors[field], rule)
}
