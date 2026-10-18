package preferences

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MergePatch (IAM-PREF-001) applies patch to stored as RFC 7396 describes: an object member of the patch replaces the stored
// one, a null removes it, and objects merge in depth; anything else (an array, a string, a number) is replaced
// whole. Numbers keep their digits. A stored value that is empty or not a JSON object counts as {}, so a column
// that holds garbage never stops a person from saving; a patch that is not a JSON object is an error.
func MergePatch(stored, patch json.RawMessage) (json.RawMessage, error) {
	p, err := decode(patch)
	if err != nil {
		return nil, fmt.Errorf("preferences: patch is not valid JSON: %w", err)
	}

	pm, ok := p.(map[string]any)
	if !ok {
		return nil, errors.New("preferences: patch is not a JSON object")
	}

	s, err := decode(stored)
	if err != nil {
		s = nil // garbage is replaced
	}

	out, err := json.Marshal(apply(s, pm))
	if err != nil {
		return nil, fmt.Errorf("preferences: encode merged object: %w", err)
	}

	return out, nil
}

// apply is the MergePatch algorithm of RFC 7396 section 2.
func apply(target, patch any) any {
	pm, ok := patch.(map[string]any)
	if !ok {
		return patch
	}

	tm, ok := target.(map[string]any)
	if !ok {
		tm = map[string]any{}
	}

	for k, v := range pm {
		if v == nil {
			delete(tm, k)

			continue
		}

		tm[k] = apply(tm[k], v)
	}

	return tm
}

// decode reads one JSON value, keeping numbers as json.Number; trailing data is an error.
func decode(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var v any

	if err := dec.Decode(&v); err != nil {
		return nil, err //nolint:wrapcheck // the caller wraps
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("data after the JSON value")
	}

	return v, nil
}
