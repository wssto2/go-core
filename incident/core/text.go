package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The tables are utf8mb3, as the first application that used them made them, so a character outside the Basic
// Multilingual Plane (an emoji) cannot be stored.
const maxBMP = 0xFFFF

// clean trims text, makes it storable and cuts it to n characters.
func clean(text string, n int) string {
	return clip(strings.TrimSpace(text), n)
}

// clip makes text storable in a utf8mb3 column and at most n characters: an invalid byte or a character outside the
// Basic Multilingual Plane becomes U+FFFD. Every bound of the module is in characters, and at three bytes a character
// the longest of them (20000) fits a TEXT column.
func clip(text string, n int) string {
	if text == "" {
		return text
	}

	var b strings.Builder

	kept := 0

	for _, r := range text {
		if kept == n {
			break
		}

		if r > maxBMP {
			r = utf8.RuneError
		}

		b.WriteRune(r)

		kept++
	}

	return b.String()
}


// Marshal is the JSON of a diagnostic document.
func Marshal(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("incident: encode %T: %w", v, err)
	}

	return string(raw), nil
}
