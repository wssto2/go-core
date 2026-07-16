package widget

import engine "github.com/wssto2/go-core/pdf"

// KeepTogetherWidget wraps a vertical block of children and, unlike Column,
// avoids splitting the block across a page boundary: if the whole block
// doesn't fit in the remaining page space, it forces a page break before
// placing any child. Falls back to placing anyway if the block is taller
// than a full page (same as Column — avoids an infinite page-break loop).
type KeepTogetherWidget struct {
	col *FlexWidget
}

// KeepTogether builds a block of children that will be kept together on one
// page whenever possible (e.g. a single invoice line item, a signature
// block) instead of being allowed to split mid-block like a plain Column.
func KeepTogether(children ...engine.Widget) *KeepTogetherWidget {
	return &KeepTogetherWidget{col: Column(children...)}
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (k *KeepTogetherWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(k, mods)
}

func (k *KeepTogetherWidget) Measure(ctx *engine.Context, c engine.Constraints) engine.Placeable {
	p := k.col.Measure(ctx, c)
	return &keepTogetherPlaceable{inner: p}
}

type keepTogetherPlaceable struct {
	inner engine.Placeable
}

func (kp *keepTogetherPlaceable) Size() engine.Size { return kp.inner.Size() }

func (kp *keepTogetherPlaceable) Place(ctx *engine.Context, x, y float64) {
	kp.PlaceFlow(ctx, x, y)
}

// PlaceFlow implements engine.FlowPlaceable: KeepTogether participates in
// vertical flow (a parent Column continues right after it), it just decides
// its own page break up front instead of per child.
func (kp *keepTogetherPlaceable) PlaceFlow(ctx *engine.Context, x, y float64) float64 {
	h := kp.inner.Size().H

	// Only force a break if the block doesn't already start at the top of
	// the page (mirrors Column/Grid's per-child page-break-avoid check) —
	// otherwise a block taller than a full page would loop forever.
	if y+h > ctx.PageBottom() && y > ctx.PageTop() {
		ctx.NewPage()
		y = ctx.PageTop()
	}

	// The inner column may still break internally (block taller than a full
	// page); propagate its real end position.
	if fc, ok := kp.inner.(engine.FlowPlaceable); ok {
		return fc.PlaceFlow(ctx, x, y)
	}

	kp.inner.Place(ctx, x, y)

	return y + h
}
