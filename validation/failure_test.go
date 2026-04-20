package validation

import (
	"testing"
)

func TestFail_SetsCode(t *testing.T) {
	f := Fail(CodeRequired)
	if f.Code != CodeRequired {
		t.Errorf("expected code %q, got %q", CodeRequired, f.Code)
	}
	if f.Params != nil {
		t.Errorf("expected nil params, got %v", f.Params)
	}
}

func TestFailWith_SetsCodeAndParams(t *testing.T) {
	params := map[string]any{"max": 10}
	f := FailWith(CodeMax, params)
	if f.Code != CodeMax {
		t.Errorf("expected code %q, got %q", CodeMax, f.Code)
	}
	if f.Params["max"] != 10 {
		t.Errorf("expected max param 10, got %v", f.Params["max"])
	}
}
