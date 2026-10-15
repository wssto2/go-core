// Package webtest holds the assertions for the wire shape of response DTOs.
//
// A DTO exists to reproduce the bytes a domain entity once emitted directly. When the entity's json tags are gone, that
// shape survives only as a string captured earlier, so the tests are golden-byte tests:
//
//	func TestFromTicketKeepsTheWire(t *testing.T) {
//	    webtest.AssertGolden(t, dto.FromTicket(sample), goldenTicket)
//	}
//
// AssertSameWire and Capture are the tools for capturing that string in the first place.
package webtest

import (
	"encoding/json"
	"testing"
)

// AssertGolden marshals got and fails unless the bytes match want exactly. The comparison is byte for byte on purpose:
// field order, omitempty and number formatting are part of what a client may depend on, and a semantic comparison would
// accept a reordered or newly omitted field.
func AssertGolden(t *testing.T, got any, want string) {
	t.Helper()

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal dto: %v", err)
	}

	if string(raw) != want {
		t.Errorf("wire mismatch\n got: %s\nwant: %s", raw, want)
	}
}

// AssertSameWire fails unless domain and dto marshal to identical bytes. It proves a mapper faithful only while the
// entity still carries its json tags: capture the bytes with Capture into a golden const, then switch to AssertGolden.
func AssertSameWire(t *testing.T, domain, dto any) {
	t.Helper()

	want, err := json.Marshal(domain)
	if err != nil {
		t.Fatalf("marshal domain: %v", err)
	}

	got, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal dto: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("dto diverges from domain wire shape\n dto:    %s\n domain: %s", got, want)
	}
}

// Capture returns v's JSON encoding, so a test can log it and the developer can paste it into a golden const. It fails
// the test on a marshal error instead of returning one, so it composes inside a t.Run body.
func Capture(t *testing.T, v any) string {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("capture golden: %v", err)
	}

	return string(raw)
}
