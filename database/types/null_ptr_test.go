package types

import (
	"database/sql/driver"
	"testing"
	"time"
)

func TestNullPtrConstructors_NilIsNullAndAValueIsKept(t *testing.T) {
	i, f, d := 7, 2.5, time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	var zero time.Time

	for name, c := range map[string]struct{ nilV, someV, want any }{
		"int":   {NewNullIntPtr(nil), NewNullIntPtr(&i), int64(7)},
		"float": {NewNullFloatPtr(nil), NewNullFloatPtr(&f), 2.5},
		"date":  {NewNullDatePtr(nil), NewNullDatePtr(&d), "2026-03-04"},
	} {
		got := map[string]any{"nil": c.nilV, "some": c.someV}
		for k, v := range got {
			val, err := v.(driver.Valuer).Value()
			if err != nil {
				t.Fatalf("%s %s: %v", name, k, err)
			}

			if want := map[string]any{"nil": nil, "some": c.want}[k]; val != want {
				t.Errorf("%s %s: got %v, want %v", name, k, val, want)
			}
		}
	}

	if v, _ := NewNullDatePtr(&zero).Value(); v != nil {
		t.Errorf("a zero time is NULL, like NewNullDate: got %v", v)
	}
}
