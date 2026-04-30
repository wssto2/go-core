package types

import (
	"testing"
	"time"
)

func TestZeroDateTime_NilValue_WritesMysql5Zero(t *testing.T) {
	d := ZeroDateTime{}
	v, err := d.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != mysql5Zero {
		t.Errorf("expected %q, got %v", mysql5Zero, v)
	}
}

func TestZeroDateTime_NonNilValue_WritesFormatted(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	d := NewZeroDateTime(ts)
	v, err := d.Value()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "2024-06-15 12:00:00" {
		t.Errorf("unexpected value: %v", v)
	}
}

func TestZeroDateTime_ScanMysql5Zero_ReturnsNil(t *testing.T) {
	var d ZeroDateTime
	if err := d.Scan(mysql5Zero); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() != nil {
		t.Errorf("expected nil, got %v", d.Get())
	}
}

func TestZeroDateTime_ScanZeroTime_ReturnsNil(t *testing.T) {
	var d ZeroDateTime
	if err := d.Scan(time.Time{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() != nil {
		t.Errorf("expected nil for zero time, got %v", d.Get())
	}
}

func TestZeroDateTime_ScanRealTime_ReturnsTime(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	var d ZeroDateTime
	if err := d.Scan(ts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() == nil || !d.Get().Equal(ts) {
		t.Errorf("expected %v, got %v", ts, d.Get())
	}
}

func TestZeroDateTime_MarshalJSON_NilIsNull(t *testing.T) {
	d := ZeroDateTime{}
	b, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("expected null JSON, got %s", b)
	}
}

func TestZeroDateTime_UnmarshalJSON_NullIsNil(t *testing.T) {
	var d ZeroDateTime
	if err := d.UnmarshalJSON([]byte("null")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() != nil {
		t.Errorf("expected nil, got %v", d.Get())
	}
}

func TestZeroDateTime_UnmarshalJSON_Mysql5ZeroStringIsNil(t *testing.T) {
	var d ZeroDateTime
	if err := d.UnmarshalJSON([]byte(`"0000-00-00 00:00:00"`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Get() != nil {
		t.Errorf("expected nil for mysql5 zero string, got %v", d.Get())
	}
}

func TestZeroDateTime_RoundTrip(t *testing.T) {
	ts := time.Date(2025, 1, 31, 8, 30, 0, 0, time.UTC)
	d := NewZeroDateTime(ts)

	b, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var d2 ZeroDateTime
	if err := d2.UnmarshalJSON(b); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if d2.Get() == nil || !d2.Get().Equal(ts) {
		t.Errorf("round-trip mismatch: expected %v, got %v", ts, d2.Get())
	}
}
