// Package binders parses HTTP request bodies and coerces values into typed
// request structs. It also runs stateless validation rules that can be
// evaluated from the field value alone, with no DB or context access needed.
//
// # Responsibility boundary
//
// The binder handles:
//   - Parsing JSON and multipart/form-data request bodies
//   - Coercing raw values (string, float64, bool) to Go struct field types
//   - Stateless validation: required, required_if, max, min, in, email, date
//
// The validator package handles:
//   - Stateful rules that need *gorm.DB: exists, unique
//   - Rules that need *gin.Context: session-based checks
//   - App-specific rules: vin, confirmed_password
//
// # Typical handler flow
//
//	// Step 1: binder parses body + runs stateless rules
//	// (done automatically by guards.BindRequest middleware)
//	request := ctx.MustGet("request").(*CreateCustomerRequest)
//
//	// Step 2: validator runs stateful rules (only when needed)
//	v := validator.New(ctx, db)
//	if err := v.Validate(request); err != nil {
//	    _ = ctx.Error(err)
//	    return
//	}
//
// # Supported validation tags
//
//	required              — field must be present and non-empty
//	required_if:field,val — required when another field equals val
//	max:N                 — max characters (string) or max value (number)
//	min:N                 — min characters (string) or min value (number)
//	in:a,b,c              — value must be one of the listed options
//	email                 — must be a valid email address
//	date                  — must be a parseable date (YYYY-MM-DD, DD.MM.YYYY, MM/DD/YYYY)
package binders

import (
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/validation"
)

// BindJSON parses and validates the request body into v.
// Supports Content-Type: application/json and multipart/form-data.
//
// Returns nil on success.
// Returns ErrValidation with field-level messages on validation failure.
// Returns ErrBadRequest if the body is unreadable or malformed JSON.
func BindJSON[T any](ctx *gin.Context, v *T) error {
	raw, isMultipart, err := parseRequest(ctx.Request)
	if err != nil {
		return err
	}
	return bind(v, raw, isMultipart)
}

// BindRaw is the testable core of BindJSON.
// Call this in unit tests to avoid needing an HTTP server or gin context.
//
//	raw := map[string]any{"vrsta": float64(1), "prezime": "Doe"}
//	var req UpdateCustomerRequest
//	err := binders.BindRaw(&req, raw, false)
func BindRaw[T any](v *T, raw map[string]any, isMultipart bool) error {
	return bind(v, raw, isMultipart)
}

func bind[T any](v *T, raw map[string]any, isMultipart bool) error {
	rv := reflect.ValueOf(v).Elem()
	rt := rv.Type()

	fields := getFieldMeta(rt)
	validationErrors := make(map[string][]validation.Failure)
	debugErrors := make(map[string][]string)

	for i := range fields {
		meta := &fields[i]
		rawVal, present := raw[meta.formKey]
		fieldVal := rv.Field(meta.index)

		if present && rawVal != nil {
			if f := coerceValue(fieldVal, rawVal, isMultipart); f != nil {
				validationErrors[meta.formKey] = append(validationErrors[meta.formKey], *f)
				debugErrors[meta.formKey] = append(debugErrors[meta.formKey], "coerce")
				continue
			}
		}
	}

	if len(validationErrors) > 0 {
		return validation.NewValidationError(
			"validation failed",
			validationErrors,
			debugErrors,
		)
	}

	return nil
}

func parseRequest(r *http.Request) (map[string]any, bool, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		return parseMultipart(r)
	}
	return parseJSON(r)
}

func parseJSON(r *http.Request) (map[string]any, bool, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, false, apperr.BadRequest("failed to read request body: " + err.Error())
	}
	if len(body) == 0 {
		return make(map[string]any), false, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, false, apperr.BadRequest("invalid JSON: " + err.Error())
	}
	return raw, false, nil
}

func parseMultipart(r *http.Request) (map[string]any, bool, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, true, apperr.BadRequest("failed to parse multipart form: " + err.Error())
	}
	raw := make(map[string]any, len(r.Form))
	for key, values := range r.Form {
		if len(values) > 0 {
			raw[key] = values[0]
		}
	}
	return raw, true, nil
}
