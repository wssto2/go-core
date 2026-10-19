package binders

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

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

// --- json tag fallback ---

func TestBindRaw_JsonTagFallback_NoFormTag(t *testing.T) {
	type Req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	raw := map[string]any{"name": "alice", "email": "alice@example.com"}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Name != "alice" {
		t.Errorf("expected name=alice, got %q", req.Name)
	}
	if req.Email != "alice@example.com" {
		t.Errorf("expected email=alice@example.com, got %q", req.Email)
	}
}

func TestBindRaw_FormTagTakesPriorityOverJsonTag(t *testing.T) {
	type Req struct {
		Name string `form:"full_name" json:"name"`
	}
	raw := map[string]any{"full_name": "bob"}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Name != "bob" {
		t.Errorf("expected Name=bob (via form tag), got %q", req.Name)
	}
}

func TestBindRaw_JsonTagOmitemptyStripped(t *testing.T) {
	type Req struct {
		Score int `json:"score,omitempty"`
	}
	raw := map[string]any{"score": float64(42)}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Score != 42 {
		t.Errorf("expected Score=42, got %d", req.Score)
	}
}

func TestBindRaw_JsonDashExcludesField(t *testing.T) {
	type Req struct {
		Secret string `json:"-"`
		Name   string `json:"name"`
	}
	raw := map[string]any{"-": "should-be-ignored", "name": "carol"}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Secret != "" {
		t.Errorf("expected Secret to remain empty, got %q", req.Secret)
	}
	if req.Name != "carol" {
		t.Errorf("expected Name=carol, got %q", req.Name)
	}
}

func TestBindRaw_ArrayOfObjects_CoercesIntoStructSlice(t *testing.T) {
	type Item struct {
		Name  string  `json:"name"`
		Price float64 `json:"price"`
	}
	type Req struct {
		Items []Item `json:"items"`
	}
	raw := map[string]any{
		"items": []interface{}{
			map[string]interface{}{"name": "tires", "price": 100.5},
			map[string]interface{}{"name": "rims", "price": 200.0},
		},
	}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(req.Items))
	}
	if req.Items[0].Name != "tires" || req.Items[0].Price != 100.5 {
		t.Errorf("unexpected item[0]: %+v", req.Items[0])
	}
	if req.Items[1].Name != "rims" || req.Items[1].Price != 200.0 {
		t.Errorf("unexpected item[1]: %+v", req.Items[1])
	}
}

// --- pointer field coercion ---

func TestBindRaw_PointerInt_JSON_CoercesRealValue(t *testing.T) {
	t.Parallel()

	type Req struct {
		BodyTypeID *int `json:"bodyTypeId"`
	}
	raw := map[string]any{"bodyTypeId": float64(42)}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.BodyTypeID == nil || *req.BodyTypeID != 42 {
		t.Fatalf("expected BodyTypeID=&42, got %v", req.BodyTypeID)
	}
}

func TestBindRaw_PointerBool_JSON_CoercesRealValue(t *testing.T) {
	t.Parallel()

	type Req struct {
		IsConfirmed *bool `json:"isConfirmed"`
	}
	raw := map[string]any{"isConfirmed": false}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.IsConfirmed == nil || *req.IsConfirmed != false {
		t.Fatalf("expected IsConfirmed=&false, got %v", req.IsConfirmed)
	}
}

func TestBindRaw_PointerField_ExplicitNull_LeavesNil(t *testing.T) {
	t.Parallel()

	type Req struct {
		BodyTypeID *int `json:"bodyTypeId"`
	}
	raw := map[string]any{"bodyTypeId": nil}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.BodyTypeID != nil {
		t.Fatalf("expected BodyTypeID to remain nil, got %v", req.BodyTypeID)
	}
}

func TestBindRaw_PointerTime_JSON_ParsesRFC3339String(t *testing.T) {
	t.Parallel()

	type Req struct {
		FirstRegistrationDate *time.Time `json:"firstRegistrationDate"`
	}
	raw := map[string]any{"firstRegistrationDate": "2019-06-01T00:00:00Z"}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.FirstRegistrationDate == nil {
		t.Fatal("expected FirstRegistrationDate to be set, got nil")
	}
	want := time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC)
	if !req.FirstRegistrationDate.Equal(want) {
		t.Fatalf("expected %v, got %v", want, *req.FirstRegistrationDate)
	}
}

func TestBindRaw_Time_JSON_ParsesRFC3339String(t *testing.T) {
	t.Parallel()

	type Req struct {
		CreatedAt time.Time `json:"createdAt"`
	}
	raw := map[string]any{"createdAt": "2019-06-01T00:00:00Z"}
	var req Req
	if err := BindRaw(&req, raw, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC)
	if !req.CreatedAt.Equal(want) {
		t.Fatalf("expected %v, got %v", want, req.CreatedAt)
	}
}

func TestBindRaw_PointerInt_InvalidType_ReturnsFailure(t *testing.T) {
	t.Parallel()

	type Req struct {
		BodyTypeID *int `json:"bodyTypeId"`
	}
	raw := map[string]any{"bodyTypeId": "not-a-number"}
	var req Req
	err := BindRaw(&req, raw, false)
	if err == nil {
		t.Fatal("expected error for JSON string into *int field")
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
