package go2ts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringUnion(t *testing.T) {
	assert.Equal(t, "export type P =\n  | 'a:b'\n  | 'c:d';\n", StringUnion("P", []string{"a:b", "c:d"}))
	assert.Equal(t, "export type P =\n  | 'a:b';\n", StringUnion("P", []string{"a:b"}))
	assert.Equal(t, "export type P = never;\n", StringUnion("P", nil))
}

func TestQuote(t *testing.T) {
	assert.Equal(t, `'plain'`, Quote("plain"))
	assert.Equal(t, `'it\'s'`, Quote("it's"))
	assert.Equal(t, `'a\\b'`, Quote(`a\b`))
	assert.Equal(t, `'a\nb'`, Quote("a\nb"))
}
