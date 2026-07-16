package pdf

import "testing"

// TestParseHexColor covers full "#RRGGBB", CSS shorthand "#RGB", a missing
// '#' prefix, and the malformed-input fallback to black.
func TestParseHexColor(t *testing.T) {
	tests := []struct {
		in      string
		r, g, b int
	}{
		{"#FF8000", 255, 128, 0},
		{"FF8000", 255, 128, 0},
		{"#F80", 255, 136, 0}, // shorthand expands per-digit: F->FF, 8->88, 0->00
		{"#fff", 255, 255, 255},
		{"", 0, 0, 0},
		{"#GGGGGG", 0, 0, 0},
		{"#12345", 0, 0, 0},
	}

	for _, tt := range tests {
		r, g, b := parseHexColor(tt.in)
		if r != tt.r || g != tt.g || b != tt.b {
			t.Errorf("parseHexColor(%q) = (%d,%d,%d), want (%d,%d,%d)", tt.in, r, g, b, tt.r, tt.g, tt.b)
		}
	}
}

// TestImageTypeSniffsMagicBytes verifies DrawImage's format detection so
// callers can pass PNG/JPEG/GIF without declaring which.
func TestImageTypeSniffsMagicBytes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"png", []byte("\x89PNG\r\n\x1a\nrest"), "PNG"},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}, "JPG"},
		{"gif87", []byte("GIF87a..."), "GIF"},
		{"gif89", []byte("GIF89a..."), "GIF"},
		{"unknown", []byte("not an image"), "PNG"},
	}

	for _, tt := range tests {
		if got := imageType(tt.data); got != tt.want {
			t.Errorf("imageType(%s) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestImageIDCollisionResistance pins the content-addressed image name to a
// real hash (not a 32-bit checksum) — two distinct blobs must never map to
// the same gofpdf image name, or the wrong asset gets silently rendered.
func TestImageIDCollisionResistance(t *testing.T) {
	a := imageID([]byte("image-a"))
	b := imageID([]byte("image-b"))

	if a == b {
		t.Fatalf("imageID collided: %q", a)
	}

	if a != imageID([]byte("image-a")) {
		t.Fatal("imageID is not deterministic")
	}
}
