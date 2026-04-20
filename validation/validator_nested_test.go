package validation

import (
	"testing"
	"time"
)

// --- Nested struct ---

type nestedItem struct {
	Name string `json:"name" validation:"required"`
}

type nestedRequest struct {
	Items []nestedItem `json:"items"`
}

func TestValidate_NestedSlice_ValidatesEachElement(t *testing.T) {
	v := New()
	req := &nestedRequest{
		Items: []nestedItem{
			{Name: "Alice"},
			{Name: ""},  // should fail required
		},
	}
	err := v.Validate(req)
	if err == nil {
		t.Fatal("expected validation error for empty nested name")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if _, exists := ve.Failures["items[1].name"]; !exists {
		t.Fatalf("expected failure at items[1].name, got %#v", ve.Failures)
	}
}

func TestValidate_NestedSlice_AllValidPasses(t *testing.T) {
	v := New()
	req := &nestedRequest{
		Items: []nestedItem{
			{Name: "Alice"},
			{Name: "Bob"},
		},
	}
	if err := v.Validate(req); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_NestedSlice_EmptySlicePasses(t *testing.T) {
	v := New()
	req := &nestedRequest{Items: []nestedItem{}}
	if err := v.Validate(req); err != nil {
		t.Fatalf("expected no error for empty slice, got %v", err)
	}
}

// --- Nested struct field (not a slice) ---

type nestedAddress struct {
	City string `json:"city" validation:"required"`
}

type nestedUserRequest struct {
	Name    string        `json:"name" validation:"required"`
	Address nestedAddress `json:"address"`
}

func TestValidate_NestedStruct_ValidatesNestedFields(t *testing.T) {
	v := New()
	req := &nestedUserRequest{
		Name:    "Alice",
		Address: nestedAddress{City: ""},
	}
	err := v.Validate(req)
	if err == nil {
		t.Fatal("expected validation error for empty nested city")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if _, exists := ve.Failures["address.city"]; !exists {
		t.Fatalf("expected failure at address.city, got %#v", ve.Failures)
	}
}

func TestValidate_NestedStruct_ValidPasses(t *testing.T) {
	v := New()
	req := &nestedUserRequest{
		Name:    "Alice",
		Address: nestedAddress{City: "Zagreb"},
	}
	if err := v.Validate(req); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// --- Pointer to nested struct ---

type nestedPtrRequest struct {
	Profile *nestedAddress `json:"profile"`
}

func TestValidate_NilPointerNestedStruct_Skipped(t *testing.T) {
	v := New()
	req := &nestedPtrRequest{Profile: nil}
	if err := v.Validate(req); err != nil {
		t.Fatalf("expected no error for nil pointer nested struct, got %v", err)
	}
}

func TestValidate_PointerNestedStruct_ValidatesFields(t *testing.T) {
	v := New()
	req := &nestedPtrRequest{Profile: &nestedAddress{City: ""}}
	err := v.Validate(req)
	if err == nil {
		t.Fatal("expected validation error for empty nested city via pointer")
	}
	ve := err.(*ValidationError)
	if _, exists := ve.Failures["profile.city"]; !exists {
		t.Fatalf("expected failure at profile.city, got %#v", ve.Failures)
	}
}

// --- Slice of pointers to structs ---

type nestedPtrSliceRequest struct {
	Tags []*nestedItem `json:"tags"`
}

func TestValidate_SliceOfPointers_ValidatesEachElement(t *testing.T) {
	v := New()
	req := &nestedPtrSliceRequest{
		Tags: []*nestedItem{
			{Name: "Go"},
			{Name: ""},
		},
	}
	err := v.Validate(req)
	if err == nil {
		t.Fatal("expected validation error")
	}
	ve := err.(*ValidationError)
	if _, exists := ve.Failures["tags[1].name"]; !exists {
		t.Fatalf("expected failure at tags[1].name, got %#v", ve.Failures)
	}
}

func TestValidate_SliceOfPointers_NilElementSkipped(t *testing.T) {
	v := New()
	req := &nestedPtrSliceRequest{
		Tags: []*nestedItem{nil},
	}
	if err := v.Validate(req); err != nil {
		t.Fatalf("expected nil element to be skipped, got %v", err)
	}
}

// --- stdlib types are not recursed into ---

type requestWithTime struct {
	Name      string    `json:"name" validation:"required"`
	CreatedAt time.Time `json:"created_at"`
}

func TestValidate_StdlibTimeField_NotRecursed(t *testing.T) {
	v := New()
	req := &requestWithTime{Name: "Alice", CreatedAt: time.Now()}
	if err := v.Validate(req); err != nil {
		t.Fatalf("expected no error, time.Time should not be recursed into: %v", err)
	}
}

// --- Deeply nested structs ---

type deepInner struct {
	Value string `json:"value" validation:"required"`
}

type deepOuter struct {
	Inner deepInner `json:"inner"`
}

type deepRoot struct {
	Data deepOuter `json:"data"`
}

func TestValidate_DeeplyNestedStruct_ValidatesAllLevels(t *testing.T) {
	v := New()
	req := &deepRoot{Data: deepOuter{Inner: deepInner{Value: ""}}}
	err := v.Validate(req)
	if err == nil {
		t.Fatal("expected validation error")
	}
	ve := err.(*ValidationError)
	if _, exists := ve.Failures["data.inner.value"]; !exists {
		t.Fatalf("expected failure at data.inner.value, got %#v", ve.Failures)
	}
}

// --- Multiple failures across nested structs ---

type multiItem struct {
	Name  string `json:"name" validation:"required"`
	Email string `json:"email" validation:"required|email"`
}

type multiRequest struct {
	Items []multiItem `json:"items"`
}

func TestValidate_NestedSlice_MultipleFailuresCollected(t *testing.T) {
	v := New()
	req := &multiRequest{
		Items: []multiItem{
			{Name: "", Email: "not-an-email"},
			{Name: "Alice", Email: ""},
		},
	}
	err := v.Validate(req)
	if err == nil {
		t.Fatal("expected validation error")
	}
	ve := err.(*ValidationError)

	expected := []string{"items[0].name", "items[0].email", "items[1].email"}
	for _, key := range expected {
		if _, exists := ve.Failures[key]; !exists {
			t.Errorf("expected failure at %q, got %#v", key, ve.Failures)
		}
	}
}
