package widget

import engine "github.com/wssto2/go-core/pdf"

// ptToMM converts a gofpdf font size (points, per gofpdf.SetFont's
// convention) into the mm units this engine's layout math uses everywhere
// else, so line-height/box-height actually correspond to the glyphs being
// drawn instead of drifting the moment FontSize changes.
const ptToMM = 0.352778

// TextAlignment controls how a TextWidget's lines are positioned within
// its measured box along the cross (horizontal) axis.
type TextAlignment int

const (
	AlignStart TextAlignment = iota
	AlignCenter
	AlignEnd
)

// defaultLineHeight is the line-height multiplier (relative to font size)
// used when no LineHeight option is given — a common comfortable default,
// matching e.g. CSS's typical body-text line-height.
const defaultLineHeight = 1.4

// TextWidget renders a (possibly multi-line, word-wrapped) string.
type TextWidget struct {
	value      string
	bold       bool
	fontSize   float64 // points, matching gofpdf.SetFont
	color      string  // hex, "" = renderer default (black)
	align      TextAlignment
	lineHeight float64 // multiplier relative to font size
}

// TextOption configures a TextWidget at construction time — text styling
// (font/weight/size/color/alignment) isn't a generic Modifier since it only
// makes sense for Text, mirroring how Compose passes TextStyle as a Text()
// parameter rather than a Modifier.
type TextOption func(*TextWidget)

// Bold renders the text in bold.
func Bold() TextOption {
	return func(t *TextWidget) { t.bold = true }
}

// FontSize sets the font size in points (default 12).
func FontSize(pt float64) TextOption {
	return func(t *TextWidget) { t.fontSize = pt }
}

// TextColor sets the text's fill color (hex, e.g. "#787878"). Default is
// black.
func TextColor(hex string) TextOption {
	return func(t *TextWidget) { t.color = hex }
}

// Center centers each line within the text's measured box along the cross
// axis (only visibly different from the default when the box is wider than
// the text itself, e.g. inside a Grid cell or under an explicit Width).
func Center() TextOption {
	return func(t *TextWidget) { t.align = AlignCenter }
}

// End right-aligns each line within the text's measured box.
func End() TextOption {
	return func(t *TextWidget) { t.align = AlignEnd }
}

// LineHeight sets the line height as a multiplier of the font size
// (default 1.4) — e.g. LineHeight(1.0) for tightly-packed lines. Values
// <= 0 fall back to the default.
func LineHeight(mult float64) TextOption {
	return func(t *TextWidget) { t.lineHeight = mult }
}

// Text builds a text widget.
func Text(value string, opts ...TextOption) *TextWidget {
	t := &TextWidget{value: value, fontSize: 12, lineHeight: defaultLineHeight}

	for _, o := range opts {
		o(t)
	}

	return t
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (t *TextWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(t, mods)
}

func (t *TextWidget) Measure(ctx *engine.Context, c engine.Constraints) engine.Placeable {
	mult := t.lineHeight
	if mult <= 0 {
		mult = defaultLineHeight
	}

	lineHeight := t.fontSize * ptToMM * mult

	var lines []string

	if c.MaxW >= engine.UnboundedThreshold {
		// Unbounded main axis (e.g. a Row's non-flex child): report the
		// natural single-line width instead of wrapping.
		lines = []string{t.value}
	} else {
		lines = ctx.Renderer.WrapText(t.value, t.bold, t.fontSize, c.MaxW)
	}

	width := 0.0

	for _, l := range lines {
		if w := ctx.Renderer.MeasureText(l, t.bold, t.fontSize); w > width {
			width = w
		}
	}

	size := c.Constrain(engine.Size{
		W: width,
		H: float64(len(lines)) * lineHeight,
	})

	return &textPlaceable{
		lines:      lines,
		bold:       t.bold,
		fontSize:   t.fontSize,
		color:      t.color,
		align:      t.align,
		lineHeight: lineHeight,
		size:       size,
	}
}

type textPlaceable struct {
	lines      []string
	bold       bool
	fontSize   float64
	color      string
	align      TextAlignment
	lineHeight float64
	size       engine.Size
}

func (tp *textPlaceable) Size() engine.Size { return tp.size }

func (tp *textPlaceable) Place(ctx *engine.Context, x, y float64) {
	if tp.align == AlignStart {
		// Fast path: all lines share the same left edge.
		ctx.Renderer.DrawTextLines(tp.lines, x, y, tp.bold, tp.fontSize, tp.lineHeight, tp.color)

		return
	}

	// Center/End alignment needs a per-line x offset, so draw one line at a
	// time rather than batching through DrawTextLines.
	for i, l := range tp.lines {
		lineW := ctx.Renderer.MeasureText(l, tp.bold, tp.fontSize)

		lx := x

		switch tp.align {
		case AlignCenter:
			lx = x + (tp.size.W-lineW)/2
		case AlignEnd:
			lx = x + (tp.size.W - lineW)
		}

		ctx.Renderer.DrawTextLines([]string{l}, lx, y+float64(i)*tp.lineHeight, tp.bold, tp.fontSize, tp.lineHeight, tp.color)
	}
}
