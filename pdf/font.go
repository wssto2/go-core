package pdf

import (
	"errors"

	"github.com/phpdave11/gofpdf"
)

// FontSet is the set of embedded UTF-8 font files an Engine uses for all
// text rendering. Family is an arbitrary name used internally to register
// the font with gofpdf (callers never need to reference it again). Regular
// and Bold are required; Light is optional (falls back to Regular wherever
// a "light" weight isn't distinguished).
//
// Using a core (non-embedded) font here would silently mangle any
// non-ASCII character, since gofpdf's core fonts only support the cp1252
// code page - not arbitrary UTF-8 - so callers must always supply a real
// UTF-8 font.
type FontSet struct {
	Family  string
	Regular []byte
	Bold    []byte
	Light   []byte
}

// validate reports whether fs is complete enough to build an Engine from.
func (fs FontSet) validate() error {
	if fs.Family == "" {
		return errors.New("pdf: FontSet.Family is required")
	}

	if len(fs.Regular) == 0 {
		return errors.New("pdf: FontSet.Regular is required")
	}

	if len(fs.Bold) == 0 {
		return errors.New("pdf: FontSet.Bold is required")
	}

	return nil
}

// fontStyle returns the gofpdf style string ("" or "B") for the given
// weight, matching the styles registered by registerFonts.
func fontStyle(bold bool) string {
	if bold {
		return "B"
	}

	return ""
}

// registerFonts embeds fs's UTF-8 font files into pdf under fs.Family.
// Without this, gofpdf falls back to treating text as cp1252, garbling any
// non-ASCII character.
func registerFonts(pdf *gofpdf.Fpdf, fs FontSet) {
	pdf.AddUTF8FontFromBytes(fs.Family, "", fs.Regular)
	pdf.AddUTF8FontFromBytes(fs.Family, "B", fs.Bold)

	if len(fs.Light) > 0 {
		pdf.AddUTF8FontFromBytes(fs.Family, "L", fs.Light)
	}
}
