package utils

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPluck_TypeMismatch_SkipsField confirms that a struct field whose type does
// not match the destination slice element type is silently skipped (no panic).
func TestPluck_TypeMismatch_SkipsField(t *testing.T) {
	type item struct {
		Name string
		Age  int
	}
	items := []item{
		{Name: "Alice", Age: 30},
		{Name: "Bob", Age: 25},
	}

	// Pluck "Age" (int) into a []string — type mismatch, should skip without panic.
	var names []string
	require.NotPanics(t, func() {
		Pluck(&items, "Age", &names)
	})
	assert.Empty(t, names, "mismatched field type should be skipped")

	// Pluck "Name" (string) into []string — should work normally.
	Pluck(&items, "Name", &names)
	assert.Equal(t, []string{"Alice", "Bob"}, names)
}

// TestToMap_UnexportedFields_NoPanic confirms that a struct containing unexported
// fields does not panic and only exported fields appear in the result.
func TestToMap_UnexportedFields_NoPanic(t *testing.T) {
	type mixed struct {
		Exported   string
		unexported string //nolint:unused
	}

	m := mixed{Exported: "visible", unexported: "hidden"}

	var result map[string]any
	require.NotPanics(t, func() {
		result = ToMap(m)
	})

	assert.Equal(t, "visible", result["Exported"])
	_, hasUnexported := result["unexported"]
	assert.False(t, hasUnexported, "unexported fields must not appear in ToMap result")
}

func TestStringClean_MultiByte_NoCorruption(t *testing.T) {
// Each Japanese character is 3 bytes; truncating at byte offset 3 would
// give only the first character, which is correct — but at byte 4 would
// produce invalid UTF-8.
input := "日本語テスト" // 6 runes, 18 bytes
result := StringClean(input, 3)

// Output must be valid UTF-8.
assert.True(t, utf8.ValidString(result), "result must be valid UTF-8")
// Exactly 3 runes.
assert.Equal(t, 3, len([]rune(result)))
assert.Equal(t, "日本語", result)
}
