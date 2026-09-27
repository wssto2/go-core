package types

import (
	"context"
	"testing"
	"time"
)

// Go's zero time.Time means "no value" exactly like nil: it must be written as
// SQL NULL, never as 0001-01-01.

func TestNullDate_GoZeroTimeWritesNull(t *testing.T) {
	zero := time.Time{}
	cases := map[string]NullDate{
		"NewNullDate": NewNullDate(zero),
		"Set": func() NullDate {
			d := NewNullDate(time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC))
			d.Set(zero)
			return d
		}(),
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := d.Value()
			if err != nil {
				t.Fatalf("Value: %v", err)
			}
			if v != nil {
				t.Errorf("Value() = %v, want nil (NULL)", v)
			}
			expr := d.GormValue(context.Background(), nil)
			if expr.SQL != "NULL" || len(expr.Vars) != 0 {
				t.Errorf("GormValue() = %q %v, want NULL", expr.SQL, expr.Vars)
			}
			if !d.IsNull() || d.Get() != nil {
				t.Errorf("zero time must read back as NULL, got %v", d.Get())
			}
		})
	}
}

func TestNullDateTime_GoZeroTimeWritesNull(t *testing.T) {
	zero := time.Time{}
	cases := map[string]NullDateTime{
		"NewNullDateTime":    NewNullDateTime(zero),
		"NewNullDateTimePtr": NewNullDateTimePtr(&zero),
		"Set": func() NullDateTime {
			d := NewNullDateTime(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
			d.Set(zero)
			return d
		}(),
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := d.Value()
			if err != nil {
				t.Fatalf("Value: %v", err)
			}
			if v != nil {
				t.Errorf("Value() = %v, want nil (NULL)", v)
			}
			expr := d.GormValue(context.Background(), nil)
			if expr.SQL != "NULL" || len(expr.Vars) != 0 {
				t.Errorf("GormValue() = %q %v, want NULL", expr.SQL, expr.Vars)
			}
			if !d.IsNull() || d.Get() != nil {
				t.Errorf("zero time must read back as NULL, got %v", d.Get())
			}
		})
	}
}
