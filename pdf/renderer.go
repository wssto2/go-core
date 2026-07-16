package pdf

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/phpdave11/gofpdf"
)

// Renderer wraps gofpdf, giving widgets a small surface for text
// measurement/drawing and shape drawing without leaking gofpdf types into
// the rest of the package.
type Renderer struct {
	pdf *gofpdf.Fpdf

	// fontFamily is the family name fonts were registered under by
	// NewRenderer, used for every SetFont call.
	fontFamily string

	// registeredImages tracks which image byte blobs have already been
	// handed to gofpdf, keyed by imageID(data) — gofpdf identifies images
	// by name, and re-registering the same name is wasted work (and, for
	// some image types, an error), so this lets the same logo/asset bytes
	// be reused across many ImageWidget instances (e.g. a repeated header
	// logo) without re-decoding every time.
	registeredImages map[string]bool
}

// NewRenderer creates a portrait A4 renderer (mm units) with fonts embedded
// into it. It fails if fonts is incomplete (see FontSet).
func NewRenderer(fonts FontSet) (*Renderer, error) {
	if err := fonts.validate(); err != nil {
		return nil, err
	}

	pdf := gofpdf.New("P", "mm", "A4", "")

	registerFonts(pdf, fonts)

	return &Renderer{
		pdf:              pdf,
		fontFamily:       fonts.Family,
		registeredImages: make(map[string]bool),
	}, nil
}

// AddPage starts a new physical page.
func (r *Renderer) AddPage() {
	r.pdf.AddPage()
}

// Output writes the finished document to w.
func (r *Renderer) Output(w io.Writer) error {
	return r.pdf.Output(w)
}

// Err surfaces any error gofpdf has accumulated so far. gofpdf swallows
// errors (bad image data, font problems, ...) into internal state and keeps
// accepting draw calls as no-ops, so checking this right after placement
// attributes failures much closer to their source than waiting for Output.
func (r *Renderer) Err() error {
	return r.pdf.Error()
}

func (r *Renderer) setFont(bold bool, sizePt float64) {
	r.pdf.SetFont(r.fontFamily, fontStyle(bold), sizePt)
}

// MeasureText returns the natural single-line width (mm) of value, set in
// the given weight/size (points, matching gofpdf.SetFont's convention).
func (r *Renderer) MeasureText(value string, bold bool, sizePt float64) float64 {
	r.setFont(bold, sizePt)

	return r.pdf.GetStringWidth(value)
}

// WrapText splits value into lines that each fit within maxWidth (mm).
func (r *Renderer) WrapText(value string, bold bool, sizePt, maxWidth float64) []string {
	r.setFont(bold, sizePt)

	raw := r.pdf.SplitLines([]byte(value), maxWidth)
	lines := make([]string, len(raw))

	for i, l := range raw {
		lines[i] = string(l)
	}

	return lines
}

// DrawTextLines draws pre-wrapped lines starting at (x, y), one lineHeight
// (mm) apart, in the given hex color (empty string means default black —
// matches gofpdf's own default so callers that never touch color get the
// same output as before this method took one).
func (r *Renderer) DrawTextLines(lines []string, x, y float64, bold bool, sizePt, lineHeight float64, color string) {
	r.setFont(bold, sizePt)
	r.setTextColor(color)

	// Approximate cap-height baseline offset so text sits inside its line
	// box instead of hanging above it.
	baseline := lineHeight * 0.75

	for i, l := range lines {
		r.pdf.Text(x, y+float64(i)*lineHeight+baseline, l)
	}
}

func (r *Renderer) setTextColor(hex string) {
	if hex == "" {
		r.pdf.SetTextColor(0, 0, 0)

		return
	}

	cr, cg, cb := parseHexColor(hex)
	r.pdf.SetTextColor(cr, cg, cb)
}

// DrawLine strokes a straight line between two points (hex color, lineWidth
// in mm) — used for divider rules and the checkbox "checked" mark.
func (r *Renderer) DrawLine(x1, y1, x2, y2 float64, color string, lineWidth float64) {
	cr, cg, cb := parseHexColor(color)
	r.pdf.SetDrawColor(cr, cg, cb)
	r.pdf.SetLineWidth(lineWidth)
	r.pdf.Line(x1, y1, x2, y2)
}

// imageID derives a stable, content-addressed gofpdf image name from raw
// image bytes, so the same asset (e.g. a repeated header logo) is only
// ever registered with gofpdf once no matter how many ImageWidgets embed
// it. SHA-256 (not a 32-bit checksum) so two distinct assets can't collide
// onto the same name and silently render the wrong image.
func imageID(data []byte) string {
	sum := sha256.Sum256(data)

	return fmt.Sprintf("img-%x", sum[:16])
}

// imageType sniffs the gofpdf ImageType from the data's magic bytes, so
// callers can pass PNG, JPEG, or GIF without declaring which. Unknown data
// defaults to PNG — gofpdf will then record a decode error, surfaced by
// Err/Output.
func imageType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "PNG"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "JPG"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "GIF"
	default:
		return "PNG"
	}
}

// DrawImage paints an image (raw PNG/JPEG/GIF bytes — the format is sniffed
// from its magic bytes) into the (w, h) mm box with its top-left corner at
// (x, y).
func (r *Renderer) DrawImage(data []byte, x, y, w, h float64) {
	name := imageID(data)

	opts := gofpdf.ImageOptions{ImageType: imageType(data), ReadDpi: false}

	if !r.registeredImages[name] {
		r.pdf.RegisterImageOptionsReader(name, opts, bytes.NewReader(data))
		r.registeredImages[name] = true
	}

	r.pdf.ImageOptions(name, x, y, w, h, false, opts, 0, "")
}

// FillRect paints a solid rectangle (hex color, e.g. "#F0F0F0").
func (r *Renderer) FillRect(x, y, w, h float64, color string) {
	cr, cg, cb := parseHexColor(color)
	r.pdf.SetFillColor(cr, cg, cb)
	r.pdf.Rect(x, y, w, h, "F")
}

// StrokeRect draws a rectangle outline (hex color, lineWidth in mm).
func (r *Renderer) StrokeRect(x, y, w, h float64, color string, lineWidth float64) {
	cr, cg, cb := parseHexColor(color)
	r.pdf.SetDrawColor(cr, cg, cb)
	r.pdf.SetLineWidth(lineWidth)
	r.pdf.Rect(x, y, w, h, "D")
}

// parseHexColor parses "#RRGGBB" or shorthand "#RGB" (leading '#'
// optional). Malformed input falls back to black — the safest visible
// default for strokes/text (an invisible no-op would hide the bug).
func parseHexColor(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")

	// Expand CSS-style shorthand: "abc" -> "aabbcc".
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}

	if len(hex) != 6 {
		return 0, 0, 0
	}

	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0
	}

	return int(v>>16) & 0xFF, int(v>>8) & 0xFF, int(v) & 0xFF
}
