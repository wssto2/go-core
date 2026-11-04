//nolint:gosmopolitan,paralleltest,testpackage // tests in the style of the package's others
package types

import (
	"testing"
	"time"
)

func TestNewDateTimeFromStringParsesEachFormAsLocalOrExplicitZone(t *testing.T) {
	local := func(h, m, s int) time.Time { return time.Date(2026, 11, 4, h, m, s, 0, time.Local) }

	tests := []struct {
		name, in string
		want     time.Time
	}{
		{"database form", "2026-11-04 13:45:30", local(13, 45, 30)},
		{"T with seconds", "2026-11-04T13:45:30", local(13, 45, 30)},
		{"T without seconds", "2026-11-04T13:45", local(13, 45, 0)},
		{"space without seconds", "2026-11-04 13:45", local(13, 45, 0)},
		{"RFC 3339 with an offset", "2026-11-04T13:45:30+02:00", time.Date(2026, 11, 4, 11, 45, 30, 0, time.UTC)},
		{"RFC 3339 in UTC", "2026-11-04T13:45:30Z", time.Date(2026, 11, 4, 13, 45, 30, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewDateTimeFromString(tt.in).Get()
			if !got.Equal(tt.want) {
				t.Fatalf("NewDateTimeFromString(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewDateTimeFromStringIsZeroForNothingItKnows(t *testing.T) {
	for _, in := range []string{"", "2026-11-04", "13:45", "yesterday", "2026-11-04T25:00"} {
		if got := NewDateTimeFromString(in).Get(); !got.IsZero() {
			t.Errorf("NewDateTimeFromString(%q) = %v, want the zero time", in, got)
		}
	}
}
