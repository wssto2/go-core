package widget

import engine "github.com/wssto2/go-core/pdf"

// CheckboxWidget draws a small square box, optionally with a checkmark
// stroked inside it — used for the legacy handover document's checklist
// items (see the pdfkit-based renderer's checkedbox.go for the equivalent
// hand-drawn glyph this replaces).
type CheckboxWidget struct {
	checked   bool
	size      float64
	color     string
	lineWidth float64
}

// Checkbox builds a checkbox glyph, default 4mm square, black outline.
func Checkbox(checked bool) *CheckboxWidget {
	return &CheckboxWidget{checked: checked, size: 4, color: "", lineWidth: 0.2}
}

// Size overrides the default 4mm square.
func (c *CheckboxWidget) Size(mm float64) *CheckboxWidget {
	c.size = mm
	return c
}

// Color overrides the default black outline/check color.
func (c *CheckboxWidget) Color(hex string) *CheckboxWidget {
	c.color = hex
	return c
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (c *CheckboxWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(c, mods)
}

func (c *CheckboxWidget) Measure(_ *engine.Context, con engine.Constraints) engine.Placeable {
	// ConstrainMax (not Constrain): a checkbox's size is intrinsic — an
	// ambient tight Min (e.g. a Grid cell's tight column width) must not
	// stretch it into a non-square box. It only ever shrinks to fit a
	// smaller Max.
	return &checkboxPlaceable{
		checked:   c.checked,
		color:     c.color,
		lineWidth: c.lineWidth,
		size:      con.ConstrainMax(engine.Size{W: c.size, H: c.size}),
	}
}

type checkboxPlaceable struct {
	checked   bool
	color     string
	lineWidth float64
	size      engine.Size
}

func (p *checkboxPlaceable) Size() engine.Size { return p.size }

func (p *checkboxPlaceable) Place(ctx *engine.Context, x, y float64) {
	ctx.Renderer.StrokeRect(x, y, p.size.W, p.size.H, p.color, p.lineWidth)

	if !p.checked {
		return
	}

	// Draw the check as two strokes (a "V" from the left edge up to the
	// bottom-middle, then up to the top-right) rather than a full X, closer
	// to a conventional checkmark glyph.
	midX, midY := x+p.size.W/2, y+p.size.H/2
	bottomY := y + p.size.H*0.75

	ctx.Renderer.DrawLine(x+p.size.W*0.15, midY, midX, bottomY, p.color, p.lineWidth*1.5)
	ctx.Renderer.DrawLine(midX, bottomY, x+p.size.W*0.9, y+p.size.H*0.2, p.color, p.lineWidth*1.5)
}
