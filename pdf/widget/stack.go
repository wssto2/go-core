package widget

import engine "github.com/wssto2/go-core/pdf"

// StackWidget overlays its children on top of each other at the same
// origin, matching Flutter's Stack convention: children are painted in the
// order given, so the last child ends up visually on top.
type StackWidget struct {
	children []engine.Widget
}

// Stack builds a widget that overlays children ...children at the same
// (x, y) origin. Its measured size is the max width/height across all
// children — the smallest box that contains every child.
func Stack(children ...engine.Widget) *StackWidget {
	return &StackWidget{children: children}
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (s *StackWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(s, mods)
}

func (s *StackWidget) Measure(ctx *engine.Context, c engine.Constraints) engine.Placeable {
	// Children are measured against the same box Stack itself was given
	// (loosened, so a smaller child doesn't get stretched to fill it), and
	// Stack's own size is the max extent among them, clamped back into c.
	loose := c
	loose.MinW = 0
	loose.MinH = 0

	placements := make([]engine.Placeable, len(s.children))

	maxW, maxH := 0.0, 0.0

	for i, ch := range s.children {
		if ch == nil {
			continue
		}

		p := ch.Measure(ctx, loose)
		placements[i] = p

		sz := p.Size()
		if sz.W > maxW {
			maxW = sz.W
		}

		if sz.H > maxH {
			maxH = sz.H
		}
	}

	size := c.Constrain(engine.Size{W: maxW, H: maxH})

	return &stackPlaceable{size: size, children: placements}
}

type stackPlaceable struct {
	size     engine.Size
	children []engine.Placeable
}

func (sp *stackPlaceable) Size() engine.Size { return sp.size }

func (sp *stackPlaceable) Place(ctx *engine.Context, x, y float64) {
	for _, p := range sp.children {
		if p == nil {
			continue
		}

		p.Place(ctx, x, y)
	}
}
