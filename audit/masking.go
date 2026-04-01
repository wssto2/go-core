package audit

import (
	"encoding/json"
	"strings"
)

// sensitiveSubstrings contains substrings used to detect sensitive JSON keys.
var sensitiveSubstrings = []string{
	"password",
	"passwd",
	"passphrase",
	"token",
	"secret",
	"ssn",
	"credit_card",
	"card_number",
	"cvv",
	"private_key",
	"api_key",
}

// Mask takes any value, marshals it to JSON, and returns a copy where
// sensitive fields (by key name) are replaced with "<redacted>".
// This approach is intentionally minimal: it operates on the JSON representation
// and thus does not require constructing typed copies.
func Mask(v any) any {
	if v == nil {
		return nil
	}
	// Fast path: avoid redundant marshal when the value is already JSON bytes.
	switch typed := v.(type) {
	case json.RawMessage:
		masked, err := MaskJSON(typed)
		if err != nil {
			return v
		}
		return json.RawMessage(masked)
	case []byte:
		masked, err := MaskJSON(typed)
		if err != nil {
			return v
		}
		return masked
	}
	// Slow path: marshal arbitrary value then unmarshal and mask.
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return v
	}
	maskRecursive(decoded)
	return decoded
}

// MaskJSON masks sensitive fields in already-serialized JSON bytes.
// It is more efficient than Mask when the caller already has JSON bytes,
// since it avoids a redundant marshal step.
// Returns the original bytes unchanged if parsing fails.
func MaskJSON(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return data, err
	}
	maskRecursive(decoded)
	return json.Marshal(decoded)
}

func maskRecursive(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSensitiveKey(k) {
				t[k] = "<redacted>"
			} else {
				maskRecursive(val)
			}
		}
	case []any:
		for i := range t {
			maskRecursive(t[i])
		}
	default:
		// primitives - nothing to do
	}
}

func isSensitiveKey(k string) bool {
	kl := strings.ToLower(k)
	for _, s := range sensitiveSubstrings {
		if strings.Contains(kl, s) {
			return true
		}
	}
	return false
}
