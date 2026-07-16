package widget

import (
	"math"

	engine "github.com/wssto2/go-core/pdf"
)

// Axis is the main axis a FlexWidget lays its children out along.
type Axis int

const (
	Vertical Axis = iota
	Horizontal
)

// FlexWidget lays out children along one axis, distributing space among
// Weight-modified children the same way Flutter's Row/Column +
// Expanded/Flexible or Compose's Row/Column + Modifier.weight() do. Row and
// Column are both just a FlexWidget with a different axis.
type FlexWidget struct {
	axis     Axis
	children []engine.Widget
	spacing  float64
}

// Column stacks children top to bottom. It's the only widget that knows how
// to break its children across pages (see flexPlaceable.Place) — build your
// document's top-level layout as a Column so overflowing content doesn't
// silently run off the bottom of the page.
func Column(children ...engine.Widget) *FlexWidget {
	return &FlexWidget{axis: Vertical, children: children}
}

// Row lays children out left to right.
func Row(children ...engine.Widget) *FlexWidget {
	return &FlexWidget{axis: Horizontal, children: children}
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (f *FlexWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(f, mods)
}

// Spacing sets a fixed gap (mm) inserted between each pair of adjacent
// children along the main axis — the same as CSS flexbox's `gap` property.
// It has no effect with 0 or 1 children, and doesn't add space before the
// first or after the last child.
func (f *FlexWidget) Spacing(v float64) *FlexWidget {
	f.spacing = v
	return f
}

func weightOf(w engine.Widget) float64 {
	if wd, ok := w.(engine.Weighted); ok {
		return wd.Weight()
	}

	return 0
}

func (f *FlexWidget) Measure(ctx *engine.Context, c engine.Constraints) engine.Placeable {
	mainMax, crossMax := c.MaxW, c.MaxH
	if f.axis == Vertical {
		mainMax, crossMax = c.MaxH, c.MaxW
	}

	mainBounded := mainMax < engine.UnboundedThreshold

	placements := make([]engine.Placeable, len(f.children))
	usedMain := 0.0
	totalWeight := 0.0

	nonNil := 0
	for _, ch := range f.children {
		if ch != nil {
			nonNil++
		}
	}

	totalGap := f.spacing * math.Max(0, float64(nonNil-1))

	// Pass 1: measure fixed (non-weighted, or weighted-but-main-unbounded)
	// children at their intrinsic size. A nil child (like Grid's nil cells)
	// is treated as an empty placeholder — skip it rather than panicking.
	for i, ch := range f.children {
		if ch == nil {
			continue
		}

		w := weightOf(ch)
		if w > 0 && mainBounded {
			totalWeight += w

			continue
		}

		p := ch.Measure(ctx, f.looseMain(crossMax))
		placements[i] = p
		usedMain += f.mainSize(p.Size())
	}

	// Pass 2: distribute whatever's left of the main axis among weighted
	// children, proportional to their weight.
	if totalWeight > 0 {
		remaining := math.Max(0, mainMax-usedMain-totalGap)

		for i, ch := range f.children {
			if ch == nil {
				continue
			}

			w := weightOf(ch)
			if w <= 0 {
				continue
			}

			share := remaining * (w / totalWeight)
			p := ch.Measure(ctx, f.tightMain(share, crossMax))
			placements[i] = p
			usedMain += f.mainSize(p.Size())
		}
	}

	crossSize := 0.0

	for _, p := range placements {
		if p == nil {
			continue
		}

		if s := f.crossSize(p.Size()); s > crossSize {
			crossSize = s
		}
	}

	size := c.Constrain(f.sizeFrom(usedMain+totalGap, crossSize))

	return &flexPlaceable{axis: f.axis, size: size, children: placements, spacing: f.spacing}
}

// looseMain gives a child as much main-axis space as it wants (0..Unbounded)
// but a fixed cross-axis budget.
func (f *FlexWidget) looseMain(cross float64) engine.Constraints {
	if f.axis == Horizontal {
		return engine.Constraints{MinW: 0, MaxW: engine.Unbounded, MinH: 0, MaxH: cross}
	}

	return engine.Constraints{MinW: 0, MaxW: cross, MinH: 0, MaxH: engine.Unbounded}
}

// tightMain pins a child's main-axis size to exactly `main` (used for
// weighted children, which must consume exactly their allotted share).
func (f *FlexWidget) tightMain(main, cross float64) engine.Constraints {
	if f.axis == Horizontal {
		return engine.Constraints{MinW: main, MaxW: main, MinH: 0, MaxH: cross}
	}

	return engine.Constraints{MinW: 0, MaxW: cross, MinH: main, MaxH: main}
}

func (f *FlexWidget) mainSize(s engine.Size) float64 {
	if f.axis == Horizontal {
		return s.W
	}

	return s.H
}

func (f *FlexWidget) crossSize(s engine.Size) float64 {
	if f.axis == Horizontal {
		return s.H
	}

	return s.W
}

func (f *FlexWidget) sizeFrom(main, cross float64) engine.Size {
	if f.axis == Horizontal {
		return engine.Size{W: main, H: cross}
	}

	return engine.Size{W: cross, H: main}
}

type flexPlaceable struct {
	axis     Axis
	size     engine.Size
	children []engine.Placeable
	spacing  float64
}

func (fp *flexPlaceable) Size() engine.Size { return fp.size }

func (fp *flexPlaceable) Place(ctx *engine.Context, x, y float64) {
	fp.PlaceFlow(ctx, x, y)
}

// PlaceFlow implements engine.FlowPlaceable: it places the children
// (breaking vertical flow across pages as needed) and returns the y
// coordinate just after the last child, on whatever page it ended up on —
// so a parent Column can keep laying out siblings right after a multi-page
// child instead of assuming everything landed on the starting page.
func (fp *flexPlaceable) PlaceFlow(ctx *engine.Context, x, y float64) float64 {
	if fp.axis == Horizontal {
		cx := x

		for _, p := range fp.children {
			if p == nil {
				continue
			}

			p.Place(ctx, cx, y)
			cx += p.Size().W + fp.spacing
		}

		return y + fp.size.H
	}

	// Vertical flow owns page-break-avoid: if a child doesn't fit in the
	// remaining space on the current page, start a new page before placing
	// it. Note this produces correct results for a Column used at (or near)
	// the top of the tree; a Column nested inside a Row that ends up
	// breaking mid-row will misalign its Row siblings, since a Row has no
	// way to react to its child spanning a page boundary. Keep
	// page-breaking Columns out of Rows.
	cy := y
	end := y

	for _, p := range fp.children {
		if p == nil {
			continue
		}

		// A flow-aware child (nested Column, Grid, KeepTogether) breaks
		// across pages by itself and reports where it actually ended, so
		// don't force a page break in front of it — let it start in the
		// remaining space and continue the cursor from its real end.
		if fc, ok := p.(engine.FlowPlaceable); ok {
			end = fc.PlaceFlow(ctx, x, cy)
			cy = end + fp.spacing

			continue
		}

		h := p.Size().H
		if cy+h > ctx.PageBottom() && cy > ctx.PageTop() {
			ctx.NewPage()
			cy = ctx.PageTop()
		}

		p.Place(ctx, x, cy)
		end = cy + h
		cy = end + fp.spacing
	}

	return end
}
