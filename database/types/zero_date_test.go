package types

import (
	"testing"
	"time"
)

func TestZeroDate_NilValue_WritesMysql5Zero(t *testing.T) {
	d := ZeroDate{}
	v, err := d.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != mysql5ZeroDate {
		t.Errorf("expected %q, got %v", mysql5ZeroDate, v)
	}
}

func TestZeroDate_NonNilValue_WritesFormatted(t *testing.T) {
	ts := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	d := NewZeroDate(ts)
	v, err := d.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "2024-06-15" {
		t.Errorf("unexpected value: %v", v)
	}
}

func TestZeroDate_TimePartStripped(t *testing.T) {
	ts := time.Date(2024, 6, 15, 14, 30, 59, 0, time.UTC)
	d := NewZeroDate(ts)
	v, err := d.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "2024-06-15" {
		t.Errorf("expected time part stripped, got %v", v)
	}
}

func TestZeroDate_ScanMysql5Zero_ReturnsNil(t *testing.T) {
	var d ZeroDate
	if err := d.Scan(mysql5ZeroDate); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() != nil {
		t.Errorf("expected nil, got %v", d.Get())
	}
}

func TestZeroDate_ScanZeroTime_ReturnsNil(t *testing.T) {
	var d ZeroDate
	if err := d.Scan(time.Time{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() != nil {
		t.Errorf("expected nil for zero time, got %v", d.Get())
	}
}

func TestZeroDate_ScanRealTime_ReturnsDate(t *testing.T) {
	ts := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	var d ZeroDate
	if err := d.Scan(ts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() == nil || !d.Get().Equal(ts) {
		t.Errorf("expected %v, got %v", ts, d.Get())
	}
}

func TestZeroDate_MarshalJSON_NilIsNull(t *testing.T) {
	d := ZeroDate{}
	b, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("expected null JSON, got %s", b)
	}
}

func TestZeroDate_UnmarshalJSON_NullIsNil(t *testing.T) {
	var d ZeroDate
	if err := d.UnmarshalJSON([]byte("null")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() != nil {
		t.Errorf("expected nil, got %v", d.Get())
	}
}

func TestZeroDate_UnmarshalJSON_Mysql5ZeroStringIsNil(t *testing.T) {
	var d ZeroDate
	if err := d.UnmarshalJSON([]byte(`"0000-00-00"`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() != nil {
		t.Errorf("expected nil for mysql5 zero string, got %v", d.Get())
	}
}

func TestZeroDate_RoundTrip(t *testing.T) {
	ts := time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)
	d := NewZeroDate(ts)

	b, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var d2 ZeroDate
	if err := d2.UnmarshalJSON(b); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if d2.Get() == nil || !d2.Get().Equal(ts) {
		t.Errorf("round-trip mismatch: expected %v, got %v", ts, d2.Get())
	}
}
