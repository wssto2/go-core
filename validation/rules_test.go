package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func collectFailures(rule func(string, any, string, bool, func(Failure), any), value any, args string, required bool) []Failure {
	var failures []Failure
	rule("field", value, args, required, func(f Failure) { failures = append(failures, f) }, nil)
	return failures
}

func TestRequiredRule_PresenceSemantics(t *testing.T) {
	t.Run("zero int is present", func(t *testing.T) {
		assert.Empty(t, collectFailures(RequiredRule, 0, "", true))
	})

	t.Run("zero float is present", func(t *testing.T) {
		assert.Empty(t, collectFailures(RequiredRule, 0.0, "", true))
	})

	t.Run("false is present", func(t *testing.T) {
		assert.Empty(t, collectFailures(RequiredRule, false, "", true))
	})

	t.Run("empty string is missing", func(t *testing.T) {
		failures := collectFailures(RequiredRule, "", "", true)
		assert.Len(t, failures, 1)
		assert.Equal(t, CodeRequired, failures[0].Code)
	})

	t.Run("whitespace string is missing", func(t *testing.T) {
		failures := collectFailures(RequiredRule, "   ", "", true)
		assert.Len(t, failures, 1)
		assert.Equal(t, CodeRequired, failures[0].Code)
	})

	t.Run("nil pointer is missing", func(t *testing.T) {
		var value *string
		failures := collectFailures(RequiredRule, value, "", true)
		assert.Len(t, failures, 1)
		assert.Equal(t, CodeRequired, failures[0].Code)
	})

	t.Run("empty slice is missing", func(t *testing.T) {
		failures := collectFailures(RequiredRule, []string{}, "", true)
		assert.Len(t, failures, 1)
		assert.Equal(t, CodeRequired, failures[0].Code)
	})

	t.Run("non-empty slice is present", func(t *testing.T) {
		assert.Empty(t, collectFailures(RequiredRule, []string{"x"}, "", true))
	})
}

func TestMinRule(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		args     string
		wantFail bool
	}{
		{"string long enough", "hello", "3", false},
		{"string too short", "hi", "3", true},
		{"int above min", 5, "3", false},
		{"int below min", 2, "3", true},
		{"int equal min", 3, "3", false},
		{"float above min", 2.1, "2", false},
		{"float below min", 1.9, "2", true},
		{"zero int allowed when min zero", 0, "0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failures := collectFailures(MinRule, tc.value, tc.args, false)
			if tc.wantFail {
				assert.NotEmpty(t, failures)
				assert.Equal(t, CodeMin, failures[0].Code)
			} else {
				assert.Empty(t, failures)
			}
		})
	}
}

func TestMinRule_MalformedParamPanicsWithInvalidConfig(t *testing.T) {
	assert.Panics(t, func() {
		collectFailures(MinRule, "hello", "notanumber", false)
	})
}

func TestMaxRule(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		args     string
		wantFail bool
	}{
		{"string within max", "hi", "5", false},
		{"string over max", "toolong", "5", true},
		{"int within max", 3, "5", false},
		{"int over max", 10, "5", true},
		{"float within max", 2.9, "3", false},
		{"float over max", 3.1, "3", true},
		{"zero int allowed when max zero", 0, "0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failures := collectFailures(MaxRule, tc.value, tc.args, false)
			if tc.wantFail {
				assert.NotEmpty(t, failures)
				assert.Equal(t, CodeMax, failures[0].Code)
			} else {
				assert.Empty(t, failures)
			}
		})
	}
}

func TestMaxRule_MalformedParamPanicsWithInvalidConfig(t *testing.T) {
	assert.Panics(t, func() {
		collectFailures(MaxRule, "hello", "bad", false)
	})
}

func TestLenRule(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		args     string
		wantFail bool
	}{
		{"string exact length", "hello", "5", false},
		{"string wrong length", "hello", "4", true},
		{"slice exact length", []string{"a", "b"}, "2", false},
		{"slice wrong length", []string{"a", "b"}, "3", true},
		{"array exact length", [3]int{1, 2, 3}, "3", false},
		{"array wrong length", [3]int{1, 2, 3}, "2", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failures := collectFailures(LenRule, tc.value, tc.args, false)
			if tc.wantFail {
				assert.NotEmpty(t, failures)
				assert.Equal(t, CodeLen, failures[0].Code)
			} else {
				assert.Empty(t, failures)
			}
		})
	}
}

func TestLenRule_UnsupportedTypeFails(t *testing.T) {
	failures := collectFailures(LenRule, 123, "3", false)
	assert.Len(t, failures, 1)
	assert.Equal(t, CodeLen, failures[0].Code)
}

func TestLenRule_MalformedParamPanicsWithInvalidConfig(t *testing.T) {
	assert.Panics(t, func() {
		collectFailures(LenRule, "hello", "bad", false)
	})
}

func TestBetweenRule(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		args     string
		wantFail bool
	}{
		{"string length within range", "hello", "3,5", false},
		{"string length below range", "hi", "3,5", true},
		{"string length above range", "toolong", "3,5", true},
		{"int within range", 5, "3,7", false},
		{"int below range", 2, "3,7", true},
		{"int above range", 8, "3,7", true},
		{"float within range", 4.5, "3,5", false},
		{"float below range", 2.9, "3,5", true},
		{"float above range", 5.1, "3,5", true},
		{"slice length within range", []string{"a", "b"}, "1,3", false},
		{"slice length above range", []string{"a", "b", "c", "d"}, "1,3", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failures := collectFailures(BetweenRule, tc.value, tc.args, false)
			if tc.wantFail {
				assert.NotEmpty(t, failures)
				assert.Equal(t, CodeBetween, failures[0].Code)
			} else {
				assert.Empty(t, failures)
			}
		})
	}
}

func TestBetweenRule_UnsupportedTypeFails(t *testing.T) {
	type sample struct{ Name string }
	failures := collectFailures(BetweenRule, sample{Name: "x"}, "1,3", false)
	assert.Len(t, failures, 1)
	assert.Equal(t, CodeBetween, failures[0].Code)
}

func TestBetweenRule_MalformedParamPanicsWithInvalidConfig(t *testing.T) {
	assert.Panics(t, func() {
		collectFailures(BetweenRule, "hello", "3", false)
	})
}

func TestBetweenRule_InvalidRangePanicsWithInvalidConfig(t *testing.T) {
	assert.Panics(t, func() {
		collectFailures(BetweenRule, "hello", "5,3", false)
	})
}

func TestInRule(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		args     string
		wantFail bool
	}{
		{"value in set", "a", "a|b|c", false},
		{"value not in set", "d", "a|b|c", true},
		{"int in set", 2, "1|2|3", false},
		{"int not in set", 5, "1|2|3", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failures := collectFailures(InRule, tc.value, tc.args, false)
			if tc.wantFail {
				assert.NotEmpty(t, failures)
				assert.Equal(t, CodeIn, failures[0].Code)
			} else {
				assert.Empty(t, failures)
			}
		})
	}
}

func TestDateRule(t *testing.T) {
	assert.Empty(t, collectFailures(DateRule, "2024-01-15", "", false))
	assert.NotEmpty(t, collectFailures(DateRule, "not-a-date", "", false))
	assert.NotEmpty(t, collectFailures(DateRule, "2024-13-01", "", false))
	assert.Empty(t, collectFailures(DateRule, "", "", false))
}

func TestDateTimeRule(t *testing.T) {
	assert.Empty(t, collectFailures(DateTimeRule, "2024-01-15T10:30:00Z", "", false))
	assert.NotEmpty(t, collectFailures(DateTimeRule, "not-a-datetime", "", false))
	assert.NotEmpty(t, collectFailures(DateTimeRule, "2024-01-15", "", false))
	assert.Empty(t, collectFailures(DateTimeRule, "", "", false))
}

func TestYearRule(t *testing.T) {
	assert.Empty(t, collectFailures(YearRule, "2024", "", false))
	assert.NotEmpty(t, collectFailures(YearRule, "24", "", false))
	assert.NotEmpty(t, collectFailures(YearRule, "abcd", "", false))
}

func TestMonthRule(t *testing.T) {
	assert.Empty(t, collectFailures(MonthRule, "5", "", false))
	assert.Empty(t, collectFailures(MonthRule, "05", "", false))
	assert.NotEmpty(t, collectFailures(MonthRule, "0", "", false))
	assert.NotEmpty(t, collectFailures(MonthRule, "13", "", false))
}

func TestPasswordRule(t *testing.T) {
	assert.Empty(t, collectFailures(PasswordRule, "abc12@def", "", false))
	assert.NotEmpty(t, collectFailures(PasswordRule, "short1!", "", false))
	assert.NotEmpty(t, collectFailures(PasswordRule, "alllowercase!!", "", false))
	assert.NotEmpty(t, collectFailures(PasswordRule, "NoSpecial12", "", false))
}

func TestURLRule(t *testing.T) {
	assert.Empty(t, collectFailures(URLRule, "https://example.com/path", "", false))
	assert.NotEmpty(t, collectFailures(URLRule, "not-a-url", "", false))
	assert.NotEmpty(t, collectFailures(URLRule, "example.com/no-scheme", "", false))
}

func TestUUIDRule(t *testing.T) {
	assert.Empty(t, collectFailures(UUIDRule, "550e8400-e29b-41d4-a716-446655440000", "", false))
	assert.NotEmpty(t, collectFailures(UUIDRule, "not-a-uuid", "", false))
}

func TestSameRule_MismatchedValuesFail(t *testing.T) {
	type subject struct {
		Password string `json:"password"`
	}

	var failures []Failure
	SameRule(
		"password_confirm",
		"different",
		"password",
		false,
		func(f Failure) { failures = append(failures, f) },
		&subject{Password: "secret123!"},
	)

	assert.Len(t, failures, 1)
	assert.Equal(t, CodeSame, failures[0].Code)
}

func TestDifferentRule_MatchingValuesFail(t *testing.T) {
	type subject struct {
		Password string `json:"password"`
	}

	var failures []Failure
	DifferentRule(
		"new_password",
		"secret123!",
		"password",
		false,
		func(f Failure) { failures = append(failures, f) },
		&subject{Password: "secret123!"},
	)

	assert.Len(t, failures, 1)
	assert.Equal(t, CodeDifferent, failures[0].Code)
}

func TestConfirmedRule_MismatchedValuesFail(t *testing.T) {
	type subject struct {
		Password string `json:"password"`
	}

	var failures []Failure
	ConfirmedRule(
		"password_confirm",
		"different",
		"password",
		false,
		func(f Failure) { failures = append(failures, f) },
		&subject{Password: "secret123!"},
	)

	assert.Len(t, failures, 1)
	assert.Equal(t, CodeConfirmed, failures[0].Code)
}

func TestSameRule_MatchingValuesPass(t *testing.T) {
	type subject struct {
		Password string `json:"password"`
	}
	failures := collectFailuresWithSubject(SameRule, "secret123!", "password", false, &subject{Password: "secret123!"})
	assert.Empty(t, failures)
}

func TestSameRule_MissingArgsPanics(t *testing.T) {
	assert.Panics(t, func() {
		collectFailures(SameRule, "value", "", false)
	})
}

func TestDifferentRule_DifferentValuesPass(t *testing.T) {
	type subject struct {
		Password string `json:"password"`
	}
	failures := collectFailuresWithSubject(DifferentRule, "new-password", "password", false, &subject{Password: "old-password"})
	assert.Empty(t, failures)
}

func TestDifferentRule_MissingArgsPanics(t *testing.T) {
	assert.Panics(t, func() {
		collectFailures(DifferentRule, "value", "", false)
	})
}

func TestConfirmedRule_MatchingValuesPass(t *testing.T) {
	type subject struct {
		Password string `json:"password"`
	}
	failures := collectFailuresWithSubject(ConfirmedRule, "secret123!", "password", false, &subject{Password: "secret123!"})
	assert.Empty(t, failures)
}

func TestConfirmedRule_MissingArgsPanics(t *testing.T) {
	assert.Panics(t, func() {
		collectFailures(ConfirmedRule, "value", "", false)
	})
}

func TestRequiredIfRule_ConditionMet_EmptyValueFails(t *testing.T) {
	type subject struct {
		Role string `json:"role"`
	}
	failures := collectFailuresWithSubject(RequiredIfRule, "", "role,admin", false, &subject{Role: "admin"})
	assert.Len(t, failures, 1)
	assert.Equal(t, CodeRequiredIf, failures[0].Code)
}

func TestRequiredIfRule_ConditionMet_PresentValuePasses(t *testing.T) {
	type subject struct {
		Role string `json:"role"`
	}
	failures := collectFailuresWithSubject(RequiredIfRule, "user@example.com", "role,admin", false, &subject{Role: "admin"})
	assert.Empty(t, failures)
}

func TestRequiredIfRule_ConditionNotMet_EmptyValuePasses(t *testing.T) {
	type subject struct {
		Role string `json:"role"`
	}
	failures := collectFailuresWithSubject(RequiredIfRule, "", "role,admin", false, &subject{Role: "user"})
	assert.Empty(t, failures)
}

func TestRequiredIfRule_MissingArgsPanics(t *testing.T) {
	type subject struct {
		Role string `json:"role"`
	}
	assert.Panics(t, func() {
		collectFailuresWithSubject(RequiredIfRule, "", "bad-args-no-comma", false, &subject{Role: "admin"})
	})
}

func TestRequiredIfRule_UnknownFieldPanics(t *testing.T) {
	type subject struct {
		Role string `json:"role"`
	}
	assert.Panics(t, func() {
		collectFailuresWithSubject(RequiredIfRule, "", "nonexistent,admin", false, &subject{Role: "admin"})
	})
}

func TestEmailRule_NilValueOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(EmailRule, nil, "", false))
}

func TestEmailRule_NilValueRequiredFails(t *testing.T) {
	failures := collectFailures(EmailRule, nil, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodeEmail, failures[0].Code)
}

func TestEmailRule_NonStringRequiredFails(t *testing.T) {
	failures := collectFailures(EmailRule, 42, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodeEmail, failures[0].Code)
}

func TestURLRule_NilOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(URLRule, nil, "", false))
}

func TestURLRule_NilRequiredFails(t *testing.T) {
	failures := collectFailures(URLRule, nil, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodeURL, failures[0].Code)
}

func TestURLRule_NonStringRequiredFails(t *testing.T) {
	failures := collectFailures(URLRule, 42, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodeURL, failures[0].Code)
}

func TestUUIDRule_NilOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(UUIDRule, nil, "", false))
}

func TestUUIDRule_NilRequiredFails(t *testing.T) {
	failures := collectFailures(UUIDRule, nil, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodeUUID, failures[0].Code)
}

func TestDateRule_NilOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(DateRule, nil, "", false))
}

func TestDateRule_NilRequiredFails(t *testing.T) {
	failures := collectFailures(DateRule, nil, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodeDate, failures[0].Code)
}

func TestDateTimeRule_NilOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(DateTimeRule, nil, "", false))
}

func TestDateTimeRule_NilRequiredFails(t *testing.T) {
	failures := collectFailures(DateTimeRule, nil, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodeDate, failures[0].Code)
}

func TestYearRule_NilOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(YearRule, nil, "", false))
}

func TestYearRule_NilRequiredFails(t *testing.T) {
	failures := collectFailures(YearRule, nil, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodeYear, failures[0].Code)
}

func TestMonthRule_NilOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(MonthRule, nil, "", false))
}

func TestMonthRule_NilRequiredFails(t *testing.T) {
	failures := collectFailures(MonthRule, nil, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodeMonth, failures[0].Code)
}

func TestPasswordRule_NilOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(PasswordRule, nil, "", false))
}

func TestPasswordRule_NilRequiredFails(t *testing.T) {
	failures := collectFailures(PasswordRule, nil, "", true)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodePassword, failures[0].Code)
}

func TestPasswordRule_TooLong(t *testing.T) {
	failures := collectFailures(PasswordRule, "abc12@toolongpassword!!", "", false)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodePassword, failures[0].Code)
}

func TestPasswordRule_MissingDigits(t *testing.T) {
	// Only one digit — need >= 2
	failures := collectFailures(PasswordRule, "abcdef1@xyz", "", false)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodePassword, failures[0].Code)
}

func TestPasswordRule_MissingSpecialChar(t *testing.T) {
	failures := collectFailures(PasswordRule, "abcdef12xyz", "", false)
	assert.NotEmpty(t, failures)
	assert.Equal(t, CodePassword, failures[0].Code)
}

func TestInRule_MissingArgsPanics(t *testing.T) {
	assert.Panics(t, func() {
		collectFailures(InRule, "a", "", false)
	})
}

func TestInRule_EmptyOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(InRule, nil, "a|b|c", false))
}

func TestMinRule_EmptyOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(MinRule, nil, "3", false))
	assert.Empty(t, collectFailures(MinRule, "", "3", false))
}

func TestMaxRule_EmptyOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(MaxRule, nil, "3", false))
	assert.Empty(t, collectFailures(MaxRule, "", "3", false))
}

func TestBetweenRule_EmptyOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(BetweenRule, nil, "3,5", false))
	assert.Empty(t, collectFailures(BetweenRule, "", "3,5", false))
}

func TestLenRule_EmptyOptionalPasses(t *testing.T) {
	assert.Empty(t, collectFailures(LenRule, nil, "3", false))
	assert.Empty(t, collectFailures(LenRule, "", "3", false))
}

// collectFailuresWithSubject is like collectFailures but accepts an explicit subject.
func collectFailuresWithSubject(rule func(string, any, string, bool, func(Failure), any), value any, args string, required bool, subject any) []Failure {
	var failures []Failure
	rule("field", value, args, required, func(f Failure) { failures = append(failures, f) }, subject)
	return failures
}

// --- isRequired + required_if integration ---

func TestValidate_RequiredIfConditionNotMet_OtherRulesSkipEmptyValue(t *testing.T) {
	// When required_if condition is NOT met and the field is empty,
	// other rules (e.g. email) must not run and must not produce failures.
	v := New()
	type Req struct {
		Role  string `json:"role"`
		Email string `json:"email" validation:"required_if:role,admin|email"`
	}

	err := v.Validate(&Req{Role: "user", Email: ""})
	assert.NoError(t, err, "email should be skipped when required_if condition is not met and value is empty")
}

func TestValidate_RequiredIfConditionMet_OtherRulesEnforceEmptyValue(t *testing.T) {
	// When required_if condition IS met and the field is empty,
	// other rules (e.g. email) should still run and produce failures.
	v := New()
	type Req struct {
		Role  string `json:"role"`
		Email string `json:"email" validation:"required_if:role,admin|email"`
	}

	err := v.Validate(&Req{Role: "admin", Email: ""})
	assert.Error(t, err)
	ve, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, ve.Failures, "email")
}
