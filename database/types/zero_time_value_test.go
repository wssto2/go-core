package types

import (
	"context"
	"testing"
	"time"
)

// Go's zero time.Time means "no value" exactly like nil: it must be written as
// the MySQL 5 zero sentinel, never as 0001-01-01.

func TestZeroDate_GoZeroTimeWritesMysql5Zero(t *testing.T) {
	zero := time.Time{}
	cases := map[string]ZeroDate{
		"NewZeroDate":    NewZeroDate(zero),
		"NewZeroDatePtr": NewZeroDatePtr(&zero),
		"Set": func() ZeroDate {
			d := NewZeroDate(time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC))
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
			if v != mysql5ZeroDate {
				t.Errorf("Value() = %v, want %q", v, mysql5ZeroDate)
			}
			expr := d.GormValue(context.Background(), nil)
			if len(expr.Vars) != 1 || expr.Vars[0] != mysql5ZeroDate {
				t.Errorf("GormValue() vars = %v, want [%q]", expr.Vars, mysql5ZeroDate)
			}
			if !d.IsNull() || d.Get() != nil {
				t.Errorf("zero time must read back as no value, got %v", d.Get())
			}
		})
	}
}

func TestZeroDateTime_GoZeroTimeWritesMysql5Zero(t *testing.T) {
	zero := time.Time{}
	cases := map[string]ZeroDateTime{
		"NewZeroDateTime":    NewZeroDateTime(zero),
		"NewZeroDateTimePtr": NewZeroDateTimePtr(&zero),
		"Set": func() ZeroDateTime {
			d := NewZeroDateTime(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
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
			if v != mysql5Zero {
				t.Errorf("Value() = %v, want %q", v, mysql5Zero)
			}
			expr := d.GormValue(context.Background(), nil)
			if len(expr.Vars) != 1 || expr.Vars[0] != mysql5Zero {
				t.Errorf("GormValue() vars = %v, want [%q]", expr.Vars, mysql5Zero)
			}
			if !d.IsNull() || d.Get() != nil {
				t.Errorf("zero time must read back as no value, got %v", d.Get())
			}
		})
	}
}
