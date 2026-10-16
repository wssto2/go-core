//nolint:exhaustruct,exhaustruct_v5,noinlineerr,tagliatelle,varnamelen // as the package's other tests
package validation_test

import (
	"errors"
	"testing"

	"github.com/wssto2/go-core/validation"
)

type storableNote struct {
	Text string `json:"note"`
}

type storableInput struct {
	Name     string         `json:"name"`
	Comment  *string        `json:"comment"`
	Count    int            `json:"count"`
	Vehicles []storableNote `json:"vehicles"`
	Skipped  string         `json:"-"`
	hidden   string
}

func TestHasUnsupportedChars(t *testing.T) {
	t.Parallel()

	for s, want := range map[string]bool{
		"":             false,
		"ž đ č ć š Ž":  false,
		"\u202eevil":   false, // 3 bytes, storable
		"a \xff b":     false, // invalid byte reads as U+FFFD, 3 bytes
		"a 🚗 b":        true,
		"👍🏽":           true,
		"UU1DJF005772": false,
	} {
		if got := validation.HasUnsupportedChars(s); got != want {
			t.Errorf("validation.HasUnsupportedChars(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestCheckStorable_Clean(t *testing.T) {
	t.Parallel()

	if err := validation.CheckStorable(&storableInput{Name: "Čćž", Vehicles: []storableNote{{Text: "ok"}}}); err != nil {
		t.Fatalf("validation.CheckStorable(clean) = %v, want nil", err)
	}

	if err := validation.CheckStorable(nil); err != nil {
		t.Fatalf("validation.CheckStorable(nil) = %v, want nil", err)
	}
}

func TestCheckStorable_Fields(t *testing.T) {
	t.Parallel()

	comment := "super 😀"
	err := validation.CheckStorable(&storableInput{
		Name:     "🚗",
		Comment:  &comment,
		Vehicles: []storableNote{{Text: "ok"}, {Text: "🔥"}},
		Skipped:  "🚗",
		hidden:   "🚗",
	})

	var verr *validation.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("validation.CheckStorable(dirty) = %v, want *validation.ValidationError", err)
	}

	for _, field := range []string{"name", "comment", "vehicles.1.note"} {
		if f := verr.Failures[field]; len(f) != 1 || f[0].Code != validation.CodeUnsupportedCharacters || f[0].Params != nil {
			t.Errorf("field %q failures = %v, want one unsupported_characters", field, f)
		}

		if d := verr.DebugFields[field]; len(d) != 1 || d[0] != "unsupported_characters" {
			t.Errorf("field %q debug = %v", field, d)
		}
	}

	if len(verr.Failures) != 3 {
		t.Errorf("failures = %v, want exactly name, comment, vehicles.1.note", verr.Failures)
	}

	if verr.Message != "validation failed" {
		t.Errorf("message = %q", verr.Message)
	}
}

func TestCheckStorable_Walk(t *testing.T) {
	t.Parallel()

	type inner struct {
		Label string `json:"label,omitempty"`
		Plain string // no json tag: the Go field name
	}

	type wrapper struct {
		Inner   inner             `json:"inner"`
		Ptr     *inner            `json:"ptr"`
		NilPtr  *inner            `json:"nil_ptr"`
		NilStr  *string           `json:"nil_str"`
		Tags    []string          `json:"tags"`
		Fixed   [2]string         `json:"fixed"`
		Any     any               `json:"any"`
		Map     map[string]string `json:"map"` // not walked, as in the application this came from
		Numeric int               `json:"numeric"`
	}

	err := validation.CheckStorable(wrapper{
		Inner: inner{Label: "😀"},
		Ptr:   &inner{Plain: "😀"},
		Tags:  []string{"a", "😀"},
		Fixed: [2]string{"😀", ""},
		Any:   inner{Label: "😀"},
		Map:   map[string]string{"k": "😀"},
	})

	var verr *validation.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want *validation.ValidationError, got %v", err)
	}

	want := []string{"inner.label", "ptr.Plain", "tags.1", "fixed.0", "any.label"}
	if len(verr.Failures) != len(want) {
		t.Errorf("failures = %v, want %v", verr.Failures, want)
	}

	for _, f := range want {
		if len(verr.Failures[f]) != 1 {
			t.Errorf("missing failure on %q: %v", f, verr.Failures)
		}
	}
}
