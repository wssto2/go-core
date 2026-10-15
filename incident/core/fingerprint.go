package core

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	numberPattern = regexp.MustCompile(`\b\d+\b`)
	uuidPattern   = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	spacePattern  = regexp.MustCompile(`\s+`)
)

// NormalizeText lowercases, trims, and collapses an error message into a
// stable shape for fingerprinting/deduplication: UUIDs become "{uuid}",
// standalone numbers become "{n}", and runs of whitespace collapse to a
// single space. This lets two occurrences of "user 123 not found" and
// "user 456 not found" hash to the same fingerprint.
//
// INC-FINGERPRINT-001.
func NormalizeText(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = uuidPattern.ReplaceAllString(s, "{uuid}")
	s = numberPattern.ReplaceAllString(s, "{n}")
	s = spacePattern.ReplaceAllString(s, " ")

	return s
}

// Fingerprint normalizes and joins the given parts (skipping empty ones
// after normalization) and returns their SHA-256 hex digest. Two incidents
// with the same normalized parts collapse to the same fingerprint, which is
// how repeat occurrences of the same underlying bug get merged into one
// incident instead of creating a new row each time.
//
// INC-FINGERPRINT-001.
func Fingerprint(parts ...string) string {
	normalized := make([]string, 0, len(parts))

	for _, part := range parts {
		if trimmed := NormalizeText(part); trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}

	sum := sha256.Sum256([]byte(strings.Join(normalized, "|")))

	return hex.EncodeToString(sum[:])
}
