package validation

import (
	"testing"
)

func TestValidator_Register_ReturnsErrorOnDuplicate(t *testing.T) {
	v := New()
	err := v.Register("required", RequiredRule)
	if err == nil {
		t.Fatal("expected error on duplicate rule registration, got nil")
	}
}

func TestValidator_MustRegister_PanicsOnDuplicate(t *testing.T) {
	v := New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate rule registration, got none")
		}
	}()
	v.MustRegister("required", RequiredRule)
}
