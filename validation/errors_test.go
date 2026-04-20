package validation

import (
	"errors"
	"strings"
	"testing"
)

func TestErrUnknownRule_Error(t *testing.T) {
	err := NewErrUnknownRule("custom_rule", "email")
	msg := err.Error()
	if !strings.Contains(msg, "custom_rule") {
		t.Errorf("expected rule name in error message, got: %s", msg)
	}
	if !strings.Contains(msg, "email") {
		t.Errorf("expected field name in error message, got: %s", msg)
	}
}

func TestErrInvalidRuleConfig_Error_WithReason(t *testing.T) {
	err := NewErrInvalidRuleConfig("min", "age", "must be positive")
	msg := err.Error()
	if !strings.Contains(msg, "min") {
		t.Errorf("expected rule name in error message, got: %s", msg)
	}
	if !strings.Contains(msg, "age") {
		t.Errorf("expected field name in error message, got: %s", msg)
	}
	if !strings.Contains(msg, "must be positive") {
		t.Errorf("expected reason in error message, got: %s", msg)
	}
}

func TestErrInvalidRuleConfig_Error_WithoutReason(t *testing.T) {
	err := NewErrInvalidRuleConfig("max", "count", "")
	msg := err.Error()
	if !strings.Contains(msg, "max") {
		t.Errorf("expected rule name in error message, got: %s", msg)
	}
	if !strings.Contains(msg, "count") {
		t.Errorf("expected field name in error message, got: %s", msg)
	}
}

func TestValidationError_Unwrap_ReturnsAppError(t *testing.T) {
	ve := NewValidationError("bad input", nil, nil)
	if ve.AppError == nil {
		t.Fatal("expected AppError to be set")
	}
	unwrapped := ve.Unwrap()
	if unwrapped == nil {
		t.Fatal("Unwrap should return non-nil AppError")
	}
	if unwrapped != ve.AppError {
		t.Fatal("Unwrap should return the embedded AppError")
	}
}

func TestValidationError_ErrorsAs(t *testing.T) {
	ve := NewValidationError("bad input", map[string][]Failure{
		"email": {Fail(CodeEmail)},
	}, map[string][]string{
		"email": {"email"},
	})

	var target *ValidationError
	if !errors.As(ve, &target) {
		t.Fatal("expected errors.As to match *ValidationError")
	}
	if len(target.Failures["email"]) == 0 {
		t.Fatal("expected email failures to be present")
	}
}

func TestNewValidationError_ClonesInputMaps(t *testing.T) {
	fieldFailures := map[string][]Failure{
		"name": {Fail(CodeRequired)},
	}
	debugFields := map[string][]string{
		"name": {"required"},
	}

	ve := NewValidationError("test", fieldFailures, debugFields)

	// Mutate the originals — the ValidationError should be unaffected.
	delete(fieldFailures, "name")
	delete(debugFields, "name")

	if len(ve.Failures) == 0 {
		t.Error("ValidationError.Failures should be a defensive copy")
	}
	if len(ve.DebugFields) == 0 {
		t.Error("ValidationError.DebugFields should be a defensive copy")
	}
}
