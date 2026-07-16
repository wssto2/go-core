package widget

import engine "github.com/wssto2/go-core/pdf"

// SpacerWidget reserves a fixed amount of space along whichever axis its
// parent flex container is laid out on. It reports the same size on both
// axes so it works inside either a Row or a Column without needing a
// direction-specific variant.
type SpacerWidget struct {
	size float64
}

// Spacer reserves `size` mm of space.
func Spacer(size float64) *SpacerWidget {
	return &SpacerWidget{size: size}
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (s *SpacerWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(s, mods)
}

func (s *SpacerWidget) Measure(_ *engine.Context, c engine.Constraints) engine.Placeable {
	// ConstrainMax: a spacer's size is intrinsic and must not be stretched
	// by an ambient tight Min (e.g. inside a Grid cell).
	return &spacerPlaceable{size: c.ConstrainMax(engine.Size{W: s.size, H: s.size})}
}

type spacerPlaceable struct {
	size engine.Size
}

func (p *spacerPlaceable) Size() engine.Size { return p.size }

func (p *spacerPlaceable) Place(_ *engine.Context, _, _ float64) {}
