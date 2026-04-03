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

func TestMinRule_MalformedParam_ReturnsFailure(t *testing.T) {
	failures := collectFailures(MinRule, "hello", "notanumber", false)
	assert.NotEmpty(t, failures, "malformed min param should produce a validation failure")
	assert.Equal(t, CodeMin, failures[0].Code)
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

func TestMaxRule_MalformedParam_ReturnsFailure(t *testing.T) {
	failures := collectFailures(MaxRule, "hello", "bad", false)
	assert.NotEmpty(t, failures, "malformed max param should produce a validation failure")
	assert.Equal(t, CodeMax, failures[0].Code)
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
	// empty and not required: passes
	assert.Empty(t, collectFailures(DateRule, "", "", false))
}

func TestDateTimeRule(t *testing.T) {
	assert.Empty(t, collectFailures(DateTimeRule, "2024-01-15T10:30:00Z", "", false))
	assert.NotEmpty(t, collectFailures(DateTimeRule, "not-a-datetime", "", false))
	assert.NotEmpty(t, collectFailures(DateTimeRule, "2024-01-15", "", false))
	// empty and not required: passes
	assert.Empty(t, collectFailures(DateTimeRule, "", "", false))
}
