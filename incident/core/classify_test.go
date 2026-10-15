package core_test

import (
	"testing"

	"github.com/wssto2/go-core/incident/core"
)

func TestStatus_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status core.Status
		want   bool
	}{
		{core.StatusNew, true},
		{core.StatusTriaged, true},
		{core.StatusInProgress, true},
		{core.StatusResolved, true},
		{core.StatusIgnored, true},
		{core.StatusDuplicate, true},
		{core.Status(""), false},
		{core.Status("bogus"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()

			if got := tt.status.Valid(); got != tt.want {
				t.Errorf("Status(%q).Valid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatus_IsOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status core.Status
		want   bool
	}{
		{core.StatusNew, true},
		{core.StatusTriaged, true},
		{core.StatusInProgress, true},
		{core.StatusResolved, false},
		{core.StatusIgnored, false},
		{core.StatusDuplicate, false},
		{core.Status("bogus"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()

			if got := tt.status.IsOpen(); got != tt.want {
				t.Errorf("Status(%q).IsOpen() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestSeverity_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		severity core.Severity
		want     bool
	}{
		{core.SeverityLow, true},
		{core.SeverityMedium, true},
		{core.SeverityHigh, true},
		{core.SeverityCritical, true},
		{core.Severity(""), false},
		{core.Severity("bogus"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			t.Parallel()

			if got := tt.severity.Valid(); got != tt.want {
				t.Errorf("Severity(%q).Valid() = %v, want %v", tt.severity, got, tt.want)
			}
		})
	}
}

func TestSeverity_OrDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		severity core.Severity
		want     core.Severity
	}{
		{"valid severity passes through", core.SeverityHigh, core.SeverityHigh},
		{"empty falls back to medium", core.Severity(""), core.SeverityMedium},
		{"invalid falls back to medium", core.Severity("bogus"), core.SeverityMedium},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.severity.OrDefault(); got != tt.want {
				t.Errorf("Severity(%q).OrDefault() = %q, want %q", tt.severity, got, tt.want)
			}
		})
	}
}

func TestSource_Merge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing core.Source
		incoming core.Source
		want     core.Source
	}{
		{"empty existing adopts incoming", core.Source(""), core.SourceUserReport, core.SourceUserReport},
		{"same source is a no-op", core.SourceUserReport, core.SourceUserReport, core.SourceUserReport},
		{"different sources collapse to mixed", core.SourceUserReport, core.SourceRuntimeError, core.SourceMixed},
		{"already mixed stays mixed", core.SourceMixed, core.SourceUserReport, core.SourceMixed},
		{"a client error on a runtime error is mixed", core.SourceRuntimeError, core.SourceClientError, core.SourceMixed},
		{"a repeated client error stays one", core.SourceClientError, core.SourceClientError, core.SourceClientError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.existing.Merge(tt.incoming); got != tt.want {
				t.Errorf("Source(%q).Merge(%q) = %q, want %q", tt.existing, tt.incoming, got, tt.want)
			}
		})
	}
}
