package widget

import engine "github.com/wssto2/go-core/pdf"

// DividerWidget draws a thin horizontal rule across the full width its
// parent gives it. Like Spacer, it takes no children and reports a size on
// its own — height is the stroke's thickness plus a little breathing room
// so it doesn't visually collide with adjacent content.
type DividerWidget struct {
	color     string
	thickness float64
}

// Divider builds a full-width horizontal rule, default 0.2mm thick, in the
// given hex color (empty string = black).
func Divider(color string) *DividerWidget {
	return &DividerWidget{color: color, thickness: 0.2}
}

// Thickness overrides the default 0.2mm stroke width.
func (d *DividerWidget) Thickness(mm float64) *DividerWidget {
	d.thickness = mm
	return d
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (d *DividerWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(d, mods)
}

func (d *DividerWidget) Measure(_ *engine.Context, c engine.Constraints) engine.Placeable {
	w := c.MaxW
	if w >= engine.UnboundedThreshold {
		w = 0
	}

	// ConstrainMax: a divider's thickness is intrinsic and must not be
	// stretched by an ambient tight MinH.
	return &dividerPlaceable{
		color:     d.color,
		thickness: d.thickness,
		size:      c.ConstrainMax(engine.Size{W: w, H: d.thickness}),
	}
}

type dividerPlaceable struct {
	color     string
	thickness float64
	size      engine.Size
}

func (p *dividerPlaceable) Size() engine.Size { return p.size }

func (p *dividerPlaceable) Place(ctx *engine.Context, x, y float64) {
	midY := y + p.size.H/2
	ctx.Renderer.DrawLine(x, midY, x+p.size.W, midY, p.color, p.thickness)
}
