package types

import (
	"testing"
)

func TestNullBool_NilValue_ReturnsNil(t *testing.T) {
	b := NullBool{}
	v, err := b.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Errorf("expected nil, got %v", v)
	}
}

func TestNullBool_TrueValue_ReturnsTrue(t *testing.T) {
	b := NewNullBool(true)
	v, err := b.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != true {
		t.Errorf("expected true, got %v", v)
	}
}

func TestNullBool_FalseValue_ReturnsFalse(t *testing.T) {
	b := NewNullBool(false)
	v, err := b.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != false {
		t.Errorf("expected false, got %v", v)
	}
}

func TestNullBool_ScanNil_SetsNil(t *testing.T) {
	b := NewNullBool(true)
	if err := b.Scan(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Get() != nil {
		t.Errorf("expected nil after scanning nil")
	}
}

func TestNullBool_ScanInt64_One(t *testing.T) {
	var b NullBool
	if err := b.Scan(int64(1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Get() == nil || *b.Get() != true {
		t.Errorf("expected true for int64(1)")
	}
}

func TestNullBool_ScanInt64_Zero(t *testing.T) {
	var b NullBool
	if err := b.Scan(int64(0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Get() == nil || *b.Get() != false {
		t.Errorf("expected false for int64(0)")
	}
}

func TestNullBool_ScanByteSlice(t *testing.T) {
	var b NullBool
	if err := b.Scan([]byte("1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Get() == nil || *b.Get() != true {
		t.Errorf("expected true for []byte(\"1\")")
	}
}

func TestNullBool_MarshalJSON_NilIsNull(t *testing.T) {
	b := NullBool{}
	data, err := b.MarshalJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("expected null, got %s", data)
	}
}

func TestNullBool_MarshalJSON_True(t *testing.T) {
	b := NewNullBool(true)
	data, err := b.MarshalJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "true" {
		t.Errorf("expected true, got %s", data)
	}
}

func TestNullBool_UnmarshalJSON_Null(t *testing.T) {
	var b NullBool
	if err := b.UnmarshalJSON([]byte("null")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !b.IsNull() {
		t.Error("expected IsNull() after unmarshalling null")
	}
}

func TestNullBool_RoundTrip(t *testing.T) {
	original := NewNullBool(true)
	data, _ := original.MarshalJSON()

	var restored NullBool
	if err := restored.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if restored.Get() == nil || *restored.Get() != true {
		t.Errorf("round-trip failed: expected true")
	}
}
