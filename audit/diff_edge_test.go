//nolint:exhaustruct,exhaustruct_v5 // test fixtures
package audit_test

import (
	"testing"

	"github.com/wssto2/go-core/audit"
)

// Diff never panics: a missing state, a nil pointer or a non-struct state has no changed fields.
func TestDiffEdgeCases(t *testing.T) {
	t.Parallel()

	type state struct {
		Name    string `json:"name"`
		private int
	}

	var nilState *state

	cases := map[string][2]any{
		"untyped nil":    {nil, state{Name: "a"}},
		"typed nil":      {nilState, &state{Name: "a"}},
		"map":            {map[string]int{"a": 1}, map[string]int{"a": 2}},
		"string":         {"a", "b"},
		"different type": {state{}, struct{ Name string }{}},
	}
	for name, c := range cases {
		if got := audit.Diff(c[0], c[1]); got != nil {
			t.Errorf("%s: Diff = %v, want nil", name, got)
		}
	}

	if got := audit.Diff(state{Name: "a", private: 1}, state{Name: "b", private: 2}); len(got) != 1 || got[0] != "name" {
		t.Errorf("Diff = %v, want [name] (unexported fields skipped)", got)
	}
}
