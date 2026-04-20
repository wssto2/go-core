package validation

import (
	"errors"
	"testing"

	"github.com/wssto2/go-core/apperr"
)

func TestValidateInput_Valid(t *testing.T) {
	type Req struct {
		Email string `json:"email" validation:"required|email"`
	}
	if err := ValidateInput(&Req{Email: "user@example.com"}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateInput_Invalid_ReturnsValidationError(t *testing.T) {
	type Req struct {
		Email string `json:"email" validation:"required|email"`
	}
	err := ValidateInput(&Req{Email: "not-an-email"})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if _, exists := ve.Failures["email"]; !exists {
		t.Error("expected email failures in ValidationError")
	}
}

func TestValidateInput_UnknownRule_ReturnsBadRequest(t *testing.T) {
	type Req struct {
		Name string `validation:"no_such_rule"`
	}
	err := ValidateInput(&Req{Name: "hello"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperr.AppError, got %T", err)
	}
	if appErr.Code != apperr.CodeBadRequest {
		t.Errorf("expected CodeBadRequest, got %v", appErr.Code)
	}
}

func TestValidateInput_NilPointer_ReturnsBadRequest(t *testing.T) {
	type Req struct {
		Name string `validation:"required"`
	}
	err := ValidateInput((*Req)(nil))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperr.AppError, got %T", err)
	}
	if appErr.Code != apperr.CodeBadRequest {
		t.Errorf("expected CodeBadRequest, got %v", appErr.Code)
	}
}
