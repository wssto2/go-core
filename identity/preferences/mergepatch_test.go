package preferences_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/identity/preferences"
)

// The cases of RFC 7396 appendix A, then the ones identity adds for a stored value that is not an object.
func TestMergePatch(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ name, stored, patch, want string }{
		{"replace a member", `{"a":"b"}`, `{"a":"c"}`, `{"a":"c"}`},
		{"add a member", `{"a":"b"}`, `{"b":"c"}`, `{"a":"b","b":"c"}`},
		{"null removes a member", `{"a":"b"}`, `{"a":null}`, `{}`},
		{"null removes one of two", `{"a":"b","b":"c"}`, `{"a":null}`, `{"b":"c"}`},
		{"an array is replaced", `{"a":["b"]}`, `{"a":"c"}`, `{"a":"c"}`},
		{"a value replaces an array", `{"a":"c"}`, `{"a":["b"]}`, `{"a":["b"]}`},
		{"objects merge in depth", `{"a":{"b":"c"}}`, `{"a":{"b":"d","c":null}}`, `{"a":{"b":"d"}}`},
		{"a nested object is made", `{}`, `{"a":{"bb":{"cc":null}}}`, `{"a":{"bb":{}}}`},
		{"an array in the patch is kept whole", `{"a":[{"b":"c"}]}`, `{"a":[1]}`, `{"a":[1]}`},
		{"a scalar replaces an object", `{"a":{"b":"c"}}`, `{"a":1}`, `{"a":1}`},
		{"false and zero are values, not removals", `{"a":true}`, `{"a":false,"b":0}`, `{"a":false,"b":0}`},
		{"a number keeps its digits", `{}`, `{"a":12345678901234567890}`, `{"a":12345678901234567890}`},
		{"nothing stored", ``, `{"dark_mode":true}`, `{"dark_mode":true}`},
		{"stored null", `null`, `{"dark_mode":true}`, `{"dark_mode":true}`},
		{"stored text that is not an object is replaced", `"x"`, `{"a":1}`, `{"a":1}`},
		{"stored garbage is replaced", `not json`, `{"a":1}`, `{"a":1}`},
		{"an empty patch changes nothing", `{"a":1}`, `{}`, `{"a":1}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := preferences.MergePatch([]byte(c.stored), []byte(c.patch))
			require.NoError(t, err)
			assert.JSONEq(t, c.want, string(got))
		})
	}
}

func TestMergePatchRefusesAPatchThatIsNotAnObject(t *testing.T) {
	t.Parallel()

	for _, patch := range []string{``, `null`, `[1]`, `"x"`, `3`, `{"a":`, `{"a":1} {"b":2}`} {
		_, err := preferences.MergePatch([]byte(`{"a":1}`), []byte(patch))
		require.Error(t, err, patch)
	}
}
