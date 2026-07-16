package pdf

// Modifier decorates a Widget, the same way Flutter's Modifier/Compose's
// Modifier chain wrap a child to add spacing, painting, or sizing behavior
// without the widget itself knowing about it.
//
// Order matters, exactly like Compose: the first modifier in the chain
// becomes the outermost wrapper. `Padding(...).Background(...)` (as applied
// via Modifier(Padding(...), Background(...))) paints the background inside
// the padding — reverse the order and the background would be painted
// first/outermost, with padding added around it instead.
type Modifier struct {
	apply func(Widget) Widget
}

func newModifier(f func(Widget) Widget) Modifier {
	return Modifier{apply: f}
}

// ApplyModifiers wraps w in each modifier, first-in-list outermost. Widget
// packages built outside this package (e.g. arv-next's widget package) use
// this to implement their own `.Modifier(...)` builder methods.
func ApplyModifiers(w Widget, mods []Modifier) Widget {
	for i := len(mods) - 1; i >= 0; i-- {
		w = mods[i].apply(w)
	}

	return w
}

// Padding adds space around a widget: shrinks the constraints passed to its
// child and adds the padding back onto the reported size, so parents never
// see a smaller box than what's actually drawn (unlike the old Style-based
// engine, where padding was applied only at render time and never
// reflected in the layout pass).
func Padding(e Edges) Modifier {
	return newModifier(func(inner Widget) Widget {
		return &paddingWidget{child: inner, edges: e}
	})
}

type paddingWidget struct {
	child Widget
	edges Edges
}

func (p *paddingWidget) Measure(ctx *Context, c Constraints) Placeable {
	inner := p.child.Measure(ctx, c.shrink(p.edges))
	s := inner.Size()

	size := c.Constrain(Size{
		W: s.W + p.edges.Left + p.edges.Right,
		H: s.H + p.edges.Top + p.edges.Bottom,
	})

	return &paddingPlaceable{inner: inner, edges: p.edges, size: size}
}

type paddingPlaceable struct {
	inner Placeable
	edges Edges
	size  Size
}

func (pp *paddingPlaceable) Size() Size { return pp.size }

func (pp *paddingPlaceable) Place(ctx *Context, x, y float64) {
	pp.inner.Place(ctx, x+pp.edges.Left, y+pp.edges.Top)
}

// Background paints a solid fill (hex color, e.g. "#F0F0F0") behind a
// widget, without affecting its size.
func Background(color string) Modifier {
	return newModifier(func(inner Widget) Widget {
		return &backgroundWidget{child: inner, color: color}
	})
}

type backgroundWidget struct {
	child Widget
	color string
}

func (b *backgroundWidget) Measure(ctx *Context, c Constraints) Placeable {
	inner := b.child.Measure(ctx, c)

	return &backgroundPlaceable{inner: inner, color: b.color}
}

type backgroundPlaceable struct {
	inner Placeable
	color string
}

func (bp *backgroundPlaceable) Size() Size { return bp.inner.Size() }

func (bp *backgroundPlaceable) Place(ctx *Context, x, y float64) {
	s := bp.inner.Size()
	ctx.Renderer.FillRect(x, y, s.W, s.H, bp.color)
	bp.inner.Place(ctx, x, y)
}

// Border draws a stroked rectangle around a widget without affecting its
// size. width is in mm.
//
// Note: like CSS's border-box vs. outline, the stroke is centered on the
// reported box's edge (gofpdf's rectangle-drawing convention), so half of
// `width` is painted outside the widget's measured size. For thick borders
// packed tightly against a sibling, pad before bordering (e.g.
// `.Modifier(Padding(All(width/2)), Border(width, color))`) if the overlap
// matters visually.
func Border(width float64, color string) Modifier {
	return newModifier(func(inner Widget) Widget {
		return &borderWidget{child: inner, width: width, color: color}
	})
}

type borderWidget struct {
	child Widget
	width float64
	color string
}

func (b *borderWidget) Measure(ctx *Context, c Constraints) Placeable {
	inner := b.child.Measure(ctx, c)

	return &borderPlaceable{inner: inner, width: b.width, color: b.color}
}

type borderPlaceable struct {
	inner Placeable
	width float64
	color string
}

func (bp *borderPlaceable) Size() Size { return bp.inner.Size() }

func (bp *borderPlaceable) Place(ctx *Context, x, y float64) {
	s := bp.inner.Size()
	bp.inner.Place(ctx, x, y)
	ctx.Renderer.StrokeRect(x, y, s.W, s.H, bp.color, bp.width)
}

// BorderEdges draws a stroked line on only the sides whose Edges value is
// > 0, so e.g. BorderEdges(Edges{Bottom: 0.2}, color) draws a bottom-only
// rule (a "row divider" look) instead of a full rectangle outline. Each
// side may have its own width; a side with width 0 is skipped entirely.
//
// Like Border, the stroke is centered on the reported box's edge, so half
// of a given side's width is painted outside the widget's measured size.
func BorderEdges(edges Edges, color string) Modifier {
	return newModifier(func(inner Widget) Widget {
		return &borderEdgesWidget{child: inner, edges: edges, color: color}
	})
}

// BorderBottom is sugar for BorderEdges(Edges{Bottom: width}, color) — the
// common case of a single rule under a row/cell (e.g. table row dividers)
// without the cost/visual overlap of a full rectangle border.
func BorderBottom(width float64, color string) Modifier {
	return BorderEdges(Edges{Bottom: width}, color)
}

// BorderTop is sugar for BorderEdges(Edges{Top: width}, color).
func BorderTop(width float64, color string) Modifier {
	return BorderEdges(Edges{Top: width}, color)
}

// BorderLeft is sugar for BorderEdges(Edges{Left: width}, color).
func BorderLeft(width float64, color string) Modifier {
	return BorderEdges(Edges{Left: width}, color)
}

// BorderRight is sugar for BorderEdges(Edges{Right: width}, color).
func BorderRight(width float64, color string) Modifier {
	return BorderEdges(Edges{Right: width}, color)
}

type borderEdgesWidget struct {
	child Widget
	edges Edges
	color string
}

func (b *borderEdgesWidget) Measure(ctx *Context, c Constraints) Placeable {
	inner := b.child.Measure(ctx, c)

	return &borderEdgesPlaceable{inner: inner, edges: b.edges, color: b.color}
}

type borderEdgesPlaceable struct {
	inner Placeable
	edges Edges
	color string
}

func (bp *borderEdgesPlaceable) Size() Size { return bp.inner.Size() }

func (bp *borderEdgesPlaceable) Place(ctx *Context, x, y float64) {
	s := bp.inner.Size()
	bp.inner.Place(ctx, x, y)

	if bp.edges.Top > 0 {
		ctx.Renderer.DrawLine(x, y, x+s.W, y, bp.color, bp.edges.Top)
	}

	if bp.edges.Bottom > 0 {
		ctx.Renderer.DrawLine(x, y+s.H, x+s.W, y+s.H, bp.color, bp.edges.Bottom)
	}

	if bp.edges.Left > 0 {
		ctx.Renderer.DrawLine(x, y, x, y+s.H, bp.color, bp.edges.Left)
	}

	if bp.edges.Right > 0 {
		ctx.Renderer.DrawLine(x+s.W, y, x+s.W, y+s.H, bp.color, bp.edges.Right)
	}
}

// Width overrides a widget's measured width with a fixed value (mm).
func Width(w float64) Modifier {
	return newModifier(func(inner Widget) Widget {
		return &widthWidget{child: inner, width: w}
	})
}

type widthWidget struct {
	child Widget
	width float64
}

func (w *widthWidget) Measure(ctx *Context, c Constraints) Placeable {
	cc := c
	cc.MinW, cc.MaxW = w.width, w.width

	inner := w.child.Measure(ctx, cc)
	s := inner.Size()
	s.W = w.width

	// Constrain against the *original* incoming c, not cc: the forced width
	// must still respect what our own parent actually gave us, otherwise a
	// Width() bigger than the available space would silently overflow it
	// (the parent's flex/grid math sums up children's reported sizes
	// assuming they honor the constraints they were handed).
	return &fixedSizePlaceable{inner: inner, size: c.Constrain(s)}
}

// Height overrides a widget's measured height with a fixed value (mm).
func Height(h float64) Modifier {
	return newModifier(func(inner Widget) Widget {
		return &heightWidget{child: inner, height: h}
	})
}

type heightWidget struct {
	child  Widget
	height float64
}

func (h *heightWidget) Measure(ctx *Context, c Constraints) Placeable {
	cc := c
	cc.MinH, cc.MaxH = h.height, h.height

	inner := h.child.Measure(ctx, cc)
	s := inner.Size()
	s.H = h.height

	// See widthWidget.Measure: constrain against the original c so a
	// Height() bigger than the available space clamps instead of silently
	// overflowing the parent's layout math.
	return &fixedSizePlaceable{inner: inner, size: c.Constrain(s)}
}

type fixedSizePlaceable struct {
	inner Placeable
	size  Size
}

func (fp *fixedSizePlaceable) Size() Size { return fp.size }

func (fp *fixedSizePlaceable) Place(ctx *Context, x, y float64) {
	fp.inner.Place(ctx, x, y)
}

// Weight marks a widget as a flexible child inside a Row/Column: remaining
// space along the parent's main axis is distributed among weighted
// children in proportion to their weight — mirrors Flutter's
// Expanded/Flexible, or Compose's Modifier.weight().
//
// It only has an effect when the parent's main axis is itself bounded: a
// Row's width always is (the page width); a Column's height is only
// bounded if the Column has an explicit Height modifier applied. Inside an
// unbounded main axis, Weight is ignored and the child falls back to its
// intrinsic size (mirrors Flutter, which would otherwise throw on an
// Expanded inside an unbounded Column).
//
// Weight must be the *first* (outermost) modifier in the chain: parents
// detect it via the Weighted interface on the outermost wrapper, so e.g.
// `Modifier(Padding(...), Weight(1))` hides the weight behind the padding
// wrapper and it is silently ignored — write
// `Modifier(Weight(1), Padding(...))` instead (mirrors Flutter, where
// Expanded must be the direct child of the Row/Column).
func Weight(w float64) Modifier {
	return newModifier(func(inner Widget) Widget {
		return &weightWidget{Widget: inner, weight: w}
	})
}

type weightWidget struct {
	Widget
	weight float64
}

func (w *weightWidget) Weight() float64 { return w.weight }

// Weighted is implemented by any widget wrapped in a Weight modifier. Flex
// containers built outside this package (e.g. widget.Row/widget.Column) use
// this to detect which children should share the remaining main-axis space.
type Weighted interface {
	Weight() float64
}
