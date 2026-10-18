package preferences

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrNotObject is a patch that is not a JSON object.
var ErrNotObject = errors.New("preferences: not a JSON object")

var errTrailing = errors.New("preferences: data after the JSON value")

// MergePatch (IAM-PREF-001) applies patch to stored as RFC 7396 describes: an object member of the patch replaces
// the stored one, a null removes it, and objects merge in depth; anything else (an array, a string, a number) is
// replaced whole. Numbers keep their digits. A stored value that is empty or not a JSON object counts as {}, so a
// column that holds garbage never stops a person from saving; a patch that is not a JSON object is an error
// (ErrNotObject, or the JSON syntax error).
func MergePatch(stored, patch json.RawMessage) (json.RawMessage, error) {
	decoded, err := decode(patch)
	if err != nil {
		return nil, fmt.Errorf("preferences: patch is not valid JSON: %w", err)
	}

	changes, isObject := decoded.(map[string]any)
	if !isObject {
		return nil, ErrNotObject
	}

	current, err := decode(stored)
	if err != nil {
		current = nil // garbage is replaced
	}

	out, err := json.Marshal(apply(current, changes))
	if err != nil {
		return nil, fmt.Errorf("preferences: encode merged object: %w", err)
	}

	return out, nil
}

// apply is the MergePatch algorithm of RFC 7396 section 2.
func apply(target, patch any) any {
	changes, isObject := patch.(map[string]any)
	if !isObject {
		return patch
	}

	merged, isObject := target.(map[string]any)
	if !isObject {
		merged = map[string]any{}
	}

	for key, value := range changes {
		if value == nil {
			delete(merged, key)

			continue
		}

		merged[key] = apply(merged[key], value)
	}

	return merged
}

// decode reads one JSON value, keeping numbers as json.Number; trailing data is an error.
func decode(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var value any

	err := dec.Decode(&value)
	if err != nil {
		return nil, err //nolint:wrapcheck // the callers say what was being read
	}

	_, err = dec.Token()
	if !errors.Is(err, io.EOF) {
		return nil, errTrailing
	}

	return value, nil
}
