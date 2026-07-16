package widget

import (
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	pdf "github.com/wssto2/go-core/pdf"
)

// testFonts returns a FontSet built from the standard Go fonts (Goregular/
// Gobold), which are real embedded UTF-8 TrueType fonts — good enough to
// exercise gofpdf's UTF-8 font registration and rendering paths without
// depending on any consumer's domain-specific font assets.
func testFonts() pdf.FontSet {
	return pdf.FontSet{
		Family:  "GoTest",
		Regular: goregular.TTF,
		Bold:    gobold.TTF,
	}
}
