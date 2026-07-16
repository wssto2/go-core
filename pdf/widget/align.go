package widget

import engine "github.com/wssto2/go-core/pdf"

// AlignWidget positions a single child within whatever box its parent hands
// it, without changing the child's own measured size — useful for e.g.
// right-aligning a non-Text widget (Image, Checkbox, ...) inside a wider
// column, the same way Center()/End() do for Text.
type AlignWidget struct {
	child engine.Widget
	align TextAlignment
}

// Align wraps child so it's positioned at align (AlignStart/AlignCenter/
// AlignEnd) along the horizontal axis of the box Align itself is given.
// Align reports the *full* box size it was given (like a Box/Container
// would), not the child's own size — that's what makes the alignment
// visible; wrap Align in a Width()/Weight() modifier (or place it in a Grid
// cell) to actually give it room to align within.
func Align(child engine.Widget, align TextAlignment) *AlignWidget {
	return &AlignWidget{child: child, align: align}
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (a *AlignWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(a, mods)
}

func (a *AlignWidget) Measure(ctx *engine.Context, c engine.Constraints) engine.Placeable {
	// The child is measured loosely (it shouldn't be stretched to fill the
	// box just because the box is wide) but Align itself reports the full
	// constrained box, so there's actual room to offset the child within.
	loose := c
	loose.MinW = 0
	loose.MinH = 0

	child := a.child

	var childPlaced engine.Placeable
	if child != nil {
		childPlaced = child.Measure(ctx, loose)
	}

	childSize := engine.Size{}
	if childPlaced != nil {
		childSize = childPlaced.Size()
	}

	// Fill the box we were given — that's what makes alignment visible.
	// But an unbounded MaxW (e.g. a Row's non-flex slot) must not be
	// reported as our size, or the parent's layout math blows up; fall back
	// to the child's own width, where alignment is a no-op anyway (mirrors
	// the Divider/Image guards).
	w := c.MaxW
	if w >= engine.UnboundedThreshold {
		w = childSize.W
	}

	size := c.Constrain(engine.Size{W: w, H: childSize.H})

	return &alignPlaceable{align: a.align, size: size, child: childPlaced}
}

type alignPlaceable struct {
	align TextAlignment
	size  engine.Size
	child engine.Placeable
}

func (ap *alignPlaceable) Size() engine.Size { return ap.size }

func (ap *alignPlaceable) Place(ctx *engine.Context, x, y float64) {
	if ap.child == nil {
		return
	}

	cw := ap.child.Size().W

	cx := x

	switch ap.align {
	case AlignCenter:
		cx = x + (ap.size.W-cw)/2
	case AlignEnd:
		cx = x + (ap.size.W - cw)
	}

	ap.child.Place(ctx, cx, y)
}
