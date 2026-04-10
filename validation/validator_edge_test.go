package validation

import (
	"testing"
)

// --- Validate edge cases ---

func TestValidate_NonPointerReturnError(t *testing.T) {
	v := New()
	type Req struct {
		Name string `validation:"required"`
	}
	err := v.Validate(Req{Name: "ok"}) // not a pointer
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
	found := false
	if ue, ok := err.(ErrUnknownRule); ok {
		unknownErr = ue
		found = true
	}
	if !found {
		t.Fatalf("expected ErrUnknownRule, got %T: %v", err, err)
	}
	if unknownErr.Name != "nonexistent_rule" {
		t.Fatalf("expected rule name 'nonexistent_rule', got %q", unknownErr.Name)
	}
}

func TestValidate_FieldWithoutValidationTagSkipped(t *testing.T) {
	v := New()
	type Req struct {
		NoTag string // no validation tag — should be skipped
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
	_ = v.Validate(req)
	errs := v.GetErrors()
	if len(errs) == 0 {
		t.Fatal("expected non-empty error map from GetErrors")
	}
}

// --- RegisterOverride ---

func TestRegisterOverride_ReplacesExistingRule(t *testing.T) {
	v := New()
	// Override required to always pass.
	v.RegisterOverride("required", func(attr string, val any, args string, req bool, fail func(Failure), subj any) {
		// always pass
	})

	type Req struct {
		Name string `form:"name" validation:"required"`
	}
	req := &Req{Name: ""}
	if err := v.Validate(req); err != nil {
		t.Fatalf("expected overridden required to pass, got %v", err)
	}
}

// --- Custom rules ---

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

	v1 := NewWithRules(map[string]Rule{"no_forbidden": customRule})
	req := &Req{Word: "forbidden"}
	if err := v1.Validate(req); err == nil {
		t.Fatal("expected custom rule to fail")
	}

	// Fresh validator instance to avoid reusing state from the previous Validate call.
	v2 := NewWithRules(map[string]Rule{"no_forbidden": customRule})
	req2 := &Req{Word: "allowed"}
	if err := v2.Validate(req2); err != nil {
		t.Fatalf("expected custom rule to pass for 'allowed', got %v", err)
	}
}

// --- parseValidationTag edge cases ---

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
