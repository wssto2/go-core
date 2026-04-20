package validation

import (
	"errors"
	"sync"
	"testing"
)

func TestValidate_NonPointerReturnError(t *testing.T) {
	v := New()
	type Req struct {
		Name string `validation:"required"`
	}
	err := v.Validate(Req{Name: "ok"})
	if err == nil {
		t.Fatal("expected error for non-pointer subject")
	}
}

func TestValidate_NilPointerReturnsError(t *testing.T) {
	v := New()
	type Req struct {
		Name string `validation:"required"`
	}
	err := v.Validate((*Req)(nil))
	if err == nil {
		t.Fatal("expected error for nil pointer subject")
	}
}

func TestValidate_NonStructPointerReturnsError(t *testing.T) {
	v := New()
	value := "hello"

	err := v.Validate(&value)
	if err == nil {
		t.Fatal("expected error for non-struct pointer subject")
	}
}

func TestValidate_UnknownRuleReturnsErrUnknownRule(t *testing.T) {
	v := New()
	type Req struct {
		Name string `validation:"nonexistent_rule"`
	}
	req := &Req{Name: "hi"}
	err := v.Validate(req)
	if err == nil {
		t.Fatal("expected error for unknown rule")
	}
	var unknownErr ErrUnknownRule
	if !errors.As(err, &unknownErr) {
		t.Fatalf("expected ErrUnknownRule, got %T: %v", err, err)
	}
	if unknownErr.Name != "nonexistent_rule" {
		t.Fatalf("expected rule name 'nonexistent_rule', got %q", unknownErr.Name)
	}
}

func TestValidate_InvalidRuleConfigReturnsInternalError(t *testing.T) {
	v := New()
	type Req struct {
		Name string `form:"name" validation:"min:not-a-number"`
	}

	err := v.Validate(&Req{Name: "hello"})
	if err == nil {
		t.Fatal("expected error for invalid rule config")
	}

	var appErr interface{ Unwrap() error }
	if !errors.As(err, &appErr) {
		t.Fatalf("expected wrapped error, got %T: %v", err, err)
	}

	var configErr ErrInvalidRuleConfig
	if !errors.As(err, &configErr) {
		t.Fatalf("expected ErrInvalidRuleConfig, got %T: %v", err, err)
	}
	if configErr.Rule != "min" {
		t.Fatalf("expected rule min, got %q", configErr.Rule)
	}
	if configErr.Field != "name" {
		t.Fatalf("expected field name, got %q", configErr.Field)
	}
}

func TestValidate_FieldWithoutValidationTagSkipped(t *testing.T) {
	v := New()
	type Req struct {
		NoTag string
		Email string `validation:"required|email"`
	}
	req := &Req{Email: "user@example.com"}
	if err := v.Validate(req); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_MultipleFieldFailures(t *testing.T) {
	v := New()
	type Req struct {
		Email string `form:"email" validation:"required|email"`
		Name  string `form:"name"  validation:"required"`
	}
	req := &Req{Email: "not-an-email", Name: ""}
	err := v.Validate(req)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if _, hasEmail := ve.Failures["email"]; !hasEmail {
		t.Error("expected failures for email field")
	}
	if _, hasName := ve.Failures["name"]; !hasName {
		t.Error("expected failures for name field")
	}
}

func TestValidate_GetErrors_ReturnsFailureMap(t *testing.T) {
	v := New()
	type Req struct {
		Email string `form:"email" validation:"required|email"`
	}
	req := &Req{Email: "bad"}
	if err := v.Validate(req); err == nil {
		t.Fatal("expected validation error")
	}

	errs := v.GetErrors()
	if len(errs) == 0 {
		t.Fatal("expected non-empty error map from GetErrors")
	}

	errs["email"] = nil
	latest := v.GetErrors()
	if len(latest["email"]) == 0 {
		t.Fatal("expected GetErrors to return a defensive copy")
	}
}

func TestValidate_ReusedValidatorClearsPreviousErrors(t *testing.T) {
	v := New()
	type Req struct {
		Email string `form:"email" validation:"required|email"`
	}

	if err := v.Validate(&Req{Email: "bad"}); err == nil {
		t.Fatal("expected first validation to fail")
	}

	if err := v.Validate(&Req{Email: "user@example.com"}); err != nil {
		t.Fatalf("expected second validation to pass, got %v", err)
	}

	if len(v.GetErrors()) != 0 {
		t.Fatal("expected validator errors to be cleared between validations")
	}
}

func TestValidate_UsesJSONTagAsAttributeFallback(t *testing.T) {
	v := New()
	type Req struct {
		Email string `json:"email_address" validation:"required"`
	}

	err := v.Validate(&Req{})
	if err == nil {
		t.Fatal("expected validation error")
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if _, exists := ve.Failures["email_address"]; !exists {
		t.Fatalf("expected json tag name to be used as attribute key, got %#v", ve.Failures)
	}
}

func TestValidate_RequiredIfUsesTaggedFieldName(t *testing.T) {
	v := New()
	type Req struct {
		Role  string `json:"role" validation:"required"`
		Email string `json:"email" validation:"required_if:role,admin"`
	}

	err := v.Validate(&Req{Role: "admin"})
	if err == nil {
		t.Fatal("expected validation error")
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if _, exists := ve.Failures["email"]; !exists {
		t.Fatalf("expected required_if failure on email, got %#v", ve.Failures)
	}
}

func TestValidate_ConfirmedUsesTaggedFieldName(t *testing.T) {
	v := New()
	type Req struct {
		Password        string `json:"password" validation:"required"`
		PasswordConfirm string `json:"password_confirm" validation:"confirmed:password"`
	}

	err := v.Validate(&Req{
		Password:        "secret123!",
		PasswordConfirm: "different123!",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if _, exists := ve.Failures["password_confirm"]; !exists {
		t.Fatalf("expected confirmed failure on password_confirm, got %#v", ve.Failures)
	}
}

func TestValidate_DefaultRegistryIncludesExtendedRules(t *testing.T) {
	v := New()
	type Req struct {
		Role            string `json:"role" validation:"required"`
		Email           string `json:"email" validation:"required_if:role,admin"`
		Year            string `json:"year" validation:"year"`
		Month           string `json:"month" validation:"month"`
		Password        string `json:"password" validation:"password"`
		PasswordConfirm string `json:"password_confirm" validation:"confirmed:password"`
	}

	err := v.Validate(&Req{
		Role:            "admin",
		Email:           "",
		Year:            "20",
		Month:           "13",
		Password:        "weak",
		PasswordConfirm: "different",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}

	for _, field := range []string{"email", "year", "month", "password", "password_confirm"} {
		if _, exists := ve.Failures[field]; !exists {
			t.Fatalf("expected failure for field %q, got %#v", field, ve.Failures)
		}
	}
}

func TestValidate_FirstBatchRules(t *testing.T) {
	v := New()
	type Req struct {
		Name            string `json:"name" validation:"len:4"`
		Age             int    `json:"age" validation:"between:18,65"`
		Website         string `json:"website" validation:"url"`
		ID              string `json:"id" validation:"uuid"`
		Password        string `json:"password" validation:"required"`
		PasswordConfirm string `json:"password_confirm" validation:"same:password"`
		OldPassword     string `json:"old_password" validation:"required"`
		NewPassword     string `json:"new_password" validation:"different:old_password"`
	}

	err := v.Validate(&Req{
		Name:            "abc",
		Age:             17,
		Website:         "not-a-url",
		ID:              "not-a-uuid",
		Password:        "secret123!",
		PasswordConfirm: "different123!",
		OldPassword:     "same-password",
		NewPassword:     "same-password",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}

	for _, field := range []string{"name", "age", "website", "id", "password_confirm", "new_password"} {
		if _, exists := ve.Failures[field]; !exists {
			t.Fatalf("expected failure for field %q, got %#v", field, ve.Failures)
		}
	}
}

func TestValidate_FirstBatchRulesPass(t *testing.T) {
	v := New()
	type Req struct {
		Name            string `json:"name" validation:"len:4"`
		Age             int    `json:"age" validation:"between:18,65"`
		Website         string `json:"website" validation:"url"`
		ID              string `json:"id" validation:"uuid"`
		Password        string `json:"password" validation:"required"`
		PasswordConfirm string `json:"password_confirm" validation:"same:password"`
		OldPassword     string `json:"old_password" validation:"required"`
		NewPassword     string `json:"new_password" validation:"different:old_password"`
	}

	err := v.Validate(&Req{
		Name:            "john",
		Age:             30,
		Website:         "https://example.com/profile",
		ID:              "550e8400-e29b-41d4-a716-446655440000",
		Password:        "secret123!",
		PasswordConfirm: "secret123!",
		OldPassword:     "old-secret",
		NewPassword:     "new-secret",
	})
	if err != nil {
		t.Fatalf("expected validation to pass, got %v", err)
	}
}

func TestValidate_FirstBatchRuleConfigErrors(t *testing.T) {
	v := New()
	type Req struct {
		Name string `json:"name" validation:"len:bad"`
	}

	err := v.Validate(&Req{Name: "john"})
	if err == nil {
		t.Fatal("expected error for invalid rule config")
	}

	var configErr ErrInvalidRuleConfig
	if !errors.As(err, &configErr) {
		t.Fatalf("expected ErrInvalidRuleConfig, got %T: %v", err, err)
	}
	if configErr.Rule != "len" {
		t.Fatalf("expected rule len, got %q", configErr.Rule)
	}
}

func TestValidate_ConcurrentUseIsSafe(t *testing.T) {
	v := New()
	type Req struct {
		Email string `form:"email" validation:"required|email"`
	}

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()

			req := &Req{Email: "user@example.com"}
			if i%2 == 0 {
				req.Email = "bad"
			}

			err := v.Validate(req)
			if i%2 == 0 {
				if err == nil {
					errCh <- errors.New("expected validation error for invalid request")
				}
				return
			}

			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("unexpected concurrent validation result: %v", err)
		}
	}
}

func TestRegisterOverride_ReplacesExistingRule(t *testing.T) {
	v := New()
	v.RegisterOverride("required", func(attr string, val any, args string, req bool, fail func(Failure), subj any) {
	})

	type Req struct {
		Name string `form:"name" validation:"required"`
	}
	req := &Req{Name: ""}
	if err := v.Validate(req); err != nil {
		t.Fatalf("expected overridden required to pass, got %v", err)
	}
}

func TestNewWithRules_CustomRuleAvailable(t *testing.T) {
	customRule := func(attr string, val any, args string, req bool, fail func(Failure), subj any) {
		s, ok := val.(string)
		if ok && s == "forbidden" {
			fail(Fail(CodeInvalidType))
		}
	}

	type Req struct {
		Word string `form:"word" validation:"no_forbidden"`
	}

	v := NewWithRules(map[string]Rule{"no_forbidden": customRule})
	req := &Req{Word: "forbidden"}
	if err := v.Validate(req); err == nil {
		t.Fatal("expected custom rule to fail")
	}

	req2 := &Req{Word: "allowed"}
	if err := v.Validate(req2); err != nil {
		t.Fatalf("expected custom rule to pass for 'allowed', got %v", err)
	}
}

func TestParseValidationTag_EmptySegmentsSkipped(t *testing.T) {
	rules := parseValidationTag("required||email")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (empty segment skipped), got %d", len(rules))
	}
}

func TestParseValidationTag_RuleWithArgs(t *testing.T) {
	rules := parseValidationTag("min:5|max:100")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].name != "min" || rules[0].args != "5" {
		t.Errorf("unexpected first rule: %+v", rules[0])
	}
	if rules[1].name != "max" || rules[1].args != "100" {
		t.Errorf("unexpected second rule: %+v", rules[1])
	}
}
