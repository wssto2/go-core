package core_test

import (
	"testing"

	"github.com/wssto2/go-core/incident/core"
)

func TestNormalizeText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercases and trims", "  User Not Found  ", "user not found"},
		{"collapses whitespace", "user\t\nnot   found", "user not found"},
		{"replaces standalone numbers", "user 123 not found", "user {n} not found"},
		{
			"replaces uuids",
			"request 123e4567-e89b-12d3-a456-426614174000 failed",
			"request {uuid} failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := core.NormalizeText(tt.input); got != tt.want {
				t.Errorf("core.NormalizeText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFingerprint_SameNormalizedInputsMatch(t *testing.T) {
	t.Parallel()

	a := core.Fingerprint("runtime", "orders", "user 123 not found")
	b := core.Fingerprint("runtime", "orders", "user 456 not found")

	if a != b {
		t.Errorf("core.Fingerprint() with differing only by number = %q vs %q, want equal", a, b)
	}
}

func TestFingerprint_DifferentInputsDiffer(t *testing.T) {
	t.Parallel()

	a := core.Fingerprint("runtime", "orders", "user not found")
	b := core.Fingerprint("runtime", "invoices", "user not found")

	if a == b {
		t.Errorf("core.Fingerprint() for different affected modules produced the same hash %q", a)
	}
}

func TestFingerprint_EmptyPartsAreSkipped(t *testing.T) {
	t.Parallel()

	a := core.Fingerprint("runtime", "", "user not found")
	b := core.Fingerprint("runtime", "user not found")

	if a != b {
		t.Errorf("core.Fingerprint() with an empty part = %q, want equal to without it %q", a, b)
	}
}
