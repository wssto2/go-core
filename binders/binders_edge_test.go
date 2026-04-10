package binders

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/validation"
)

// --- parseJSON edge cases ---

func TestParseJSON_EmptyBody_ReturnsEmptyMap(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", http.NoBody)
	req.Header.Set("Content-Type", "application/json")

	raw, isMultipart, err := parseJSON(req)
	if err != nil {
		t.Fatalf("expected no error for empty body, got %v", err)
	}
	if isMultipart {
		t.Fatal("expected isMultipart=false")
	}
	if len(raw) != 0 {
		t.Fatalf("expected empty map, got %v", raw)
	}
}

func TestParseJSON_InvalidJSON_ReturnsBadRequest(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", io.NopCloser(strings.NewReader(`{invalid`)))
	req.Header.Set("Content-Type", "application/json")

	_, _, err := parseJSON(req)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	var ae *apperr.AppError
	if !isAppErr(err, &ae) || ae.Code != apperr.CodeBadRequest {
		t.Fatalf("expected BadRequest AppError, got %T: %v", err, err)
	}
}

func TestParseJSON_ValidObject_ReturnsFields(t *testing.T) {
	body := `{"name":"alice","age":30}`
	req, _ := http.NewRequest("POST", "/", io.NopCloser(strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")

	raw, isMultipart, err := parseJSON(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isMultipart {
		t.Fatal("expected isMultipart=false")
	}
	if raw["name"] != "alice" {
		t.Errorf("expected name=alice, got %v", raw["name"])
	}
}

// --- parseRequest content-type routing ---

func TestParseRequest_RoutesByContentType_JSON(t *testing.T) {
	body := `{"x":1}`
	req, _ := http.NewRequest("POST", "/", io.NopCloser(strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	_, isMultipart, err := parseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isMultipart {
		t.Fatal("expected JSON route for application/json content-type")
	}
}

func TestParseRequest_RoutesByContentType_Multipart(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("hello", "world")
	w.Close()

	req, _ := http.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())

	_, isMultipart, err := parseRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isMultipart {
		t.Fatal("expected multipart route for multipart/form-data content-type")
	}
}

// --- bind / BindRaw edge cases ---

func TestBindRaw_TypeMismatch_JSON_ReturnsValidationError(t *testing.T) {
	type Req struct {
		Age int `form:"age"`
	}
	// JSON: string sent where int expected — must fail
	raw := map[string]any{"age": "not-a-number"}
	var req Req
	err := BindRaw(&req, raw, false)
	if err == nil {
		t.Fatal("expected validation error for type mismatch")
	}
	var ve *validation.ValidationError
	if !isValidationErr(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if _, hasAge := ve.Failures["age"]; !hasAge {
		t.Error("expected failure for 'age' field")
	}
}

func TestBindRaw_Multipart_StringToInt_Coerces(t *testing.T) {
	type Req struct {
		Age int `form:"age"`
	}
	raw := map[string]any{"age": "25"} // multipart strings may arrive as string
	var req Req
	err := BindRaw(&req, raw, true) // isMultipart=true
	if err != nil {
		t.Fatalf("expected successful coercion, got %v", err)
	}
	if req.Age != 25 {
		t.Fatalf("expected Age=25, got %d", req.Age)
	}
}

func TestBindRaw_MissingFieldLeftAtZero(t *testing.T) {
	type Req struct {
		Name string `form:"name"`
		Age  int    `form:"age"`
	}
	raw := map[string]any{"name": "bob"} // age absent
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Name != "bob" {
		t.Errorf("expected name=bob, got %q", req.Name)
	}
	if req.Age != 0 {
		t.Errorf("expected age=0 (zero value), got %d", req.Age)
	}
}

func TestBindRaw_NullValue_LeavesFieldUnchanged(t *testing.T) {
	// When a JSON null is present in the raw map, bind() skips the field entirely,
	// leaving it at whatever value the struct already has.
	type Req struct {
		Name string `form:"name"`
	}
	raw := map[string]any{"name": nil}
	var req Req
	req.Name = "preset"
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// explicit null leaves the field unchanged (not zeroed)
	if req.Name != "preset" {
		t.Errorf("expected Name to remain 'preset' after null, got %q", req.Name)
	}
}

// --- coerce edge cases ---

func TestCoerceString_BoolField_JSON_RejectsString(t *testing.T) {
	type Req struct {
		Active bool `form:"active"`
	}
	// JSON sends string for a bool field → must fail
	raw := map[string]any{"active": "true"}
	var req Req
	err := BindRaw(&req, raw, false)
	if err == nil {
		t.Fatal("expected error: JSON string for bool field is not valid")
	}
}

func TestCoerceString_BoolField_Multipart_Coerces(t *testing.T) {
	type Req struct {
		Active bool `form:"active"`
	}
	raw := map[string]any{"active": "true"} // multipart
	var req Req
	if err := BindRaw(&req, raw, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req.Active {
		t.Fatal("expected Active=true after multipart coercion")
	}
}

// helpers

func isAppErr(err error, out **apperr.AppError) bool {
	if ae, ok := err.(*apperr.AppError); ok {
		*out = ae
		return true
	}
	return false
}

func isValidationErr(err error, out **validation.ValidationError) bool {
	if ve, ok := err.(*validation.ValidationError); ok {
		*out = ve
		return true
	}
	return false
}
