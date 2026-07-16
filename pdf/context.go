package pdf

import "math"

// Unbounded represents "as much space as needed" along an axis during the
// measure pass. Vertical flow is intrinsic/content-driven by default — a
// widget doesn't know or care where the page boundary is while it's being
// measured, only while it's being placed (see FlexWidget/GridWidget.Place).
const Unbounded = 1e12

// UnboundedThreshold is used to detect "effectively unbounded" constraints
// (as opposed to comparing against Unbounded exactly, which would break the
// moment any arithmetic is done on it).
const UnboundedThreshold = 1e11

// Constraints are passed down the widget tree during Measure, and a Size is
// handed back up — the same "constraints go down, sizes go up" protocol
// Flutter (BoxConstraints) and Compose (Constraints/Measurable/Placeable)
// use. All values are in mm, matching the document's page units.
type Constraints struct {
	MinW, MaxW float64
	MinH, MaxH float64
}

// Size is a widget's measured result, reported back up to its parent.
type Size struct {
	W, H float64
}

func clampf(v, minV, maxV float64) float64 {
	if maxV < minV {
		maxV = minV
	}

	if v < minV {
		return minV
	}

	if v > maxV {
		return maxV
	}

	return v
}

// Constrain clamps a measured size into this set of constraints.
//
// Note: this only clamps the *reported* size — it never clips what's
// actually drawn. If a widget's natural content (e.g. wrapped text lines)
// exceeds Max after clamping, Place still draws all of it; the parent will
// lay out the next sibling right after the (now too-small) reported box,
// so the excess content can visually overlap whatever comes next. This
// matters most when MinW/MaxW/MinH/MaxH are tightened below a widget's
// natural size (e.g. a Weight() share smaller than wrapped text needs).
// True clipping would require gofpdf clip-region support and isn't
// implemented.
func (c Constraints) Constrain(s Size) Size {
	return Size{
		W: clampf(s.W, c.MinW, c.MaxW),
		H: clampf(s.H, c.MinH, c.MaxH),
	}
}

// ConstrainMax clamps a measured size against only the upper bound (Max),
// never bumping it up to Min. Used by widgets whose size is meant to be
// intrinsic/fixed (Checkbox, Spacer, Divider, Image) so that an ambient
// tight Min (e.g. a Grid cell's tight column width) doesn't stretch them —
// unlike Constrain, which Text deliberately uses to fill/align within its
// box.
func (c Constraints) ConstrainMax(s Size) Size {
	w := s.W
	if w > c.MaxW {
		w = c.MaxW
	}

	h := s.H
	if h > c.MaxH {
		h = c.MaxH
	}

	if w < 0 {
		w = 0
	}

	if h < 0 {
		h = 0
	}

	return Size{W: w, H: h}
}

// shrink reduces the available space by the given edges — used by Padding
// to pass a smaller box down to its child.
func (c Constraints) shrink(e Edges) Constraints {
	nc := c

	if nc.MaxW < UnboundedThreshold {
		nc.MaxW = math.Max(0, nc.MaxW-e.Left-e.Right)
	}

	if nc.MinW > nc.MaxW {
		nc.MinW = nc.MaxW
	}

	if nc.MaxH < UnboundedThreshold {
		nc.MaxH = math.Max(0, nc.MaxH-e.Top-e.Bottom)
	}

	if nc.MinH > nc.MaxH {
		nc.MinH = nc.MaxH
	}

	return nc
}

// Context carries render-time state shared across the whole widget tree:
// the gofpdf-backed Renderer (for text measurement/drawing) and the current
// page geometry (so any widget can trigger a page break during Place).
type Context struct {
	Renderer *Renderer

	PageWidth  float64
	PageHeight float64
	Margin     Edges

	// Header/Footer are optional document-level chrome builders (set by
	// Engine.Render from Document.header/footer), invoked with the current
	// 1-based page number every time NewPage draws a fresh page. Nil means
	// no chrome.
	Header func(pageNum int) Widget
	Footer func(pageNum int) Widget

	// headerHeight/footerHeight are measured once (from page 1) by
	// Engine.Render and reserved on every page via PageTop/PageBottom, so
	// content never overlaps the chrome even though the chrome itself is
	// re-measured (for its possibly page-number-dependent content) on every
	// page.
	headerHeight float64
	footerHeight float64

	// PageNum is the current 1-based physical page number, incremented by
	// NewPage.
	PageNum int
}

// ContentWidth is the page width minus left/right margins — the width every
// top-level widget is measured against.
func (ctx *Context) ContentWidth() float64 {
	return ctx.PageWidth - ctx.Margin.Left - ctx.Margin.Right
}

// PageTop is the y coordinate content starts at on a (freshly added) page —
// below the reserved header space, if a header is configured.
func (ctx *Context) PageTop() float64 {
	return ctx.Margin.Top + ctx.headerHeight
}

// PageBottom is the y coordinate content must not cross — above the
// reserved footer space, if a footer is configured.
func (ctx *Context) PageBottom() float64 {
	return ctx.PageHeight - ctx.Margin.Bottom - ctx.footerHeight
}

// chromeConstraints is the box the header/footer widgets are measured
// against: full content width, unbounded height (a header/footer is
// content-driven the same way top-level Document content is).
func (ctx *Context) chromeConstraints() Constraints {
	return Constraints{MinW: 0, MaxW: ctx.ContentWidth(), MinH: 0, MaxH: Unbounded}
}

// measureChromeHeight measures fn(pageNum) once (used by Engine.Render to
// compute the reserved headerHeight/footerHeight before any page is drawn).
// A nil fn or a nil widget it returns yields zero reserved height.
func measureChromeHeight(ctx *Context, fn func(pageNum int) Widget, pageNum int) float64 {
	if fn == nil {
		return 0
	}

	w := fn(pageNum)
	if w == nil {
		return 0
	}

	return w.Measure(ctx, ctx.chromeConstraints()).Size().H
}

// drawChrome renders the current page's header/footer (if configured) at
// the top/bottom of the page ctx.PageNum now refers to.
func (ctx *Context) drawChrome() {
	if ctx.Header != nil {
		if w := ctx.Header(ctx.PageNum); w != nil {
			p := w.Measure(ctx, ctx.chromeConstraints())
			p.Place(ctx, ctx.Margin.Left, ctx.Margin.Top)
		}
	}

	if ctx.Footer != nil {
		if w := ctx.Footer(ctx.PageNum); w != nil {
			p := w.Measure(ctx, ctx.chromeConstraints())
			y := ctx.PageHeight - ctx.Margin.Bottom - p.Size().H
			p.Place(ctx, ctx.Margin.Left, y)
		}
	}
}

// NewPage starts a new physical PDF page, advances PageNum, and draws any
// configured header/footer chrome onto it.
func (ctx *Context) NewPage() {
	ctx.Renderer.AddPage()
	ctx.PageNum++
	ctx.drawChrome()
}
