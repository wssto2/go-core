package core

import "testing"

func TestStatus_Valid(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusNew, true},
		{StatusTriaged, true},
		{StatusInProgress, true},
		{StatusResolved, true},
		{StatusIgnored, true},
		{StatusDuplicate, true},
		{Status(""), false},
		{Status("bogus"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.want {
				t.Errorf("Status(%q).Valid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatus_IsOpen(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusNew, true},
		{StatusTriaged, true},
		{StatusInProgress, true},
		{StatusResolved, false},
		{StatusIgnored, false},
		{StatusDuplicate, false},
		{Status("bogus"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsOpen(); got != tt.want {
				t.Errorf("Status(%q).IsOpen() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestSeverity_Valid(t *testing.T) {
	tests := []struct {
		severity Severity
		want     bool
	}{
		{SeverityLow, true},
		{SeverityMedium, true},
		{SeverityHigh, true},
		{SeverityCritical, true},
		{Severity(""), false},
		{Severity("bogus"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			if got := tt.severity.Valid(); got != tt.want {
				t.Errorf("Severity(%q).Valid() = %v, want %v", tt.severity, got, tt.want)
			}
		})
	}
}

func TestSeverity_OrDefault(t *testing.T) {
	tests := []struct {
		name     string
		severity Severity
		want     Severity
	}{
		{"valid severity passes through", SeverityHigh, SeverityHigh},
		{"empty falls back to medium", Severity(""), SeverityMedium},
		{"invalid falls back to medium", Severity("bogus"), SeverityMedium},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.severity.OrDefault(); got != tt.want {
				t.Errorf("Severity(%q).OrDefault() = %q, want %q", tt.severity, got, tt.want)
			}
		})
	}
}

func TestSource_Merge(t *testing.T) {
	tests := []struct {
		name     string
		existing Source
		incoming Source
		want     Source
	}{
		{"empty existing adopts incoming", Source(""), SourceUserReport, SourceUserReport},
		{"same source is a no-op", SourceUserReport, SourceUserReport, SourceUserReport},
		{"different sources collapse to mixed", SourceUserReport, SourceRuntimeError, SourceMixed},
		{"already mixed stays mixed", SourceMixed, SourceUserReport, SourceMixed},
		{"a client error on a runtime error is mixed", SourceRuntimeError, SourceClientError, SourceMixed},
		{"a client error repeated stays a client error", SourceClientError, SourceClientError, SourceClientError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.existing.Merge(tt.incoming); got != tt.want {
				t.Errorf("Source(%q).Merge(%q) = %q, want %q", tt.existing, tt.incoming, got, tt.want)
			}
		})
	}
}
