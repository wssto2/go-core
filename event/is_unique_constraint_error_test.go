package event

import (
	"fmt"
	"testing"
)

func TestIsUniqueConstraintError_NarrowMatching(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   bool
	}{
		{"sqlite unique", "UNIQUE constraint failed: idempotency_records.key", true},
		{"mysql duplicate", "Error 1062: Duplicate entry 'abc' for key 'PRIMARY'", true},
		{"postgres dup key", "duplicate key value violates unique constraint \"idx_key\"", true},
		{"generic unique word", "this value must be unique in the set", false},
		{"constraint failed alone", "constraint failed: some other reason", false},
		{"unrelated error", "connection refused", false},
		{"nil error", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.errMsg != "" {
				err = fmt.Errorf("%s", tc.errMsg)
			}
			got := isUniqueConstraintError(err)
			if got != tc.want {
				t.Errorf("isUniqueConstraintError(%q) = %v, want %v", tc.errMsg, got, tc.want)
			}
		})
	}
}
