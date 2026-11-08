// Package binders parses HTTP request bodies and coerces values into typed
// request structs.
//
// # Responsibility boundary
//
// The binder handles:
//   - Parsing JSON and multipart/form-data request bodies
//   - Coercing raw values (string, float64, bool) to Go struct field types
//
// # Typical handler flow
//
//	// Step 1: binder parses body
//	request := ctx.MustGet("request").(*CreateCustomerRequest)
//
//	// Step 2: validator runs stateful rules (only when needed)
//	v := validator.New(ctx, db)
//	if err := v.Validate(request); err != nil {
//	    _ = ctx.Error(err)
//	    return
//	}
package binders

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/validation"
)

// BindRequest parses and validates the request body into v.
// Supports Content-Type: application/json and multipart/form-data.
//
// Returns nil on success.
// Returns ErrValidation with field-level messages on validation failure.
// Returns ErrBadRequest if the body is unreadable or malformed JSON.
func BindRequest[T any](ctx *gin.Context, v *T) error {
	raw, isMultipart, err := parseRequest(ctx.Request, reflect.TypeFor[T]())
	if err != nil {
		return err
	}
	return bind(v, raw, isMultipart)
}

// BindRaw is the testable core of BindRequest.
// Call this in unit tests to avoid needing an HTTP server or gin context.
//
//	raw := map[string]any{"vrsta": float64(1), "prezime": "Doe"}
//	var req UpdateCustomerRequest
//	err := binders.BindRaw(&req, raw, false)
func BindRaw[T any](v *T, raw map[string]any, isMultipart bool) error {
	return bind(v, raw, isMultipart)
}

func bind[T any](subject *T, raw map[string]any, isMultipart bool) error {
	reflectValue := reflect.ValueOf(subject).Elem()
	reflectType := reflectValue.Type()

	fields := getFieldMeta(reflectType)
	validationErrors := make(map[string][]validation.Failure)
	debugErrors := make(map[string][]string)

	for i := range fields {
		meta := &fields[i]
		rawVal, present := raw[meta.formKey]
		fieldVal := reflectValue.FieldByIndex(meta.index)

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

// parseRequest reads the body; into is the struct it will be bound to (nil: no raw members).
func parseRequest(req *http.Request, into reflect.Type) (map[string]any, bool, error) {
	contentType := req.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		return parseMultipart(req)
	}

	return parseJSON(req, into)
}

// parseJSON decodes the body into a tree of strings, float64s, bools, lists and maps. A member that
// holds a json.RawMessage somewhere inside (see keepsRaw) stays a json.RawMessage of the body's own
// bytes in the tree, and coerceValue decodes it when it meets the field.
func parseJSON(r *http.Request, into reflect.Type) (map[string]any, bool, error) {
	// The body limit is the application's (web.BodyLimit); a body over it fails the read.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
			return nil, false, apperr.PayloadTooLarge("request body too large")
		}

		return nil, false, apperr.BadRequest("failed to read request body: " + err.Error())
	}

	if len(body) == 0 {
		return make(map[string]any), false, nil
	}

	if keys := rawKeys(into); len(keys) > 0 {
		var members map[string]json.RawMessage

		err := json.Unmarshal(body, &members)
		if err != nil {
			return nil, false, apperr.BadRequest("invalid JSON: " + err.Error())
		}

		raw := make(map[string]any, len(members))

		for key, member := range members {
			if keys[key] {
				raw[key] = member

				continue
			}

			var value any

			err := json.Unmarshal(member, &value)
			if err != nil {
				return nil, false, apperr.BadRequest("invalid JSON: " + err.Error())
			}

			raw[key] = value
		}

		return raw, false, nil
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
