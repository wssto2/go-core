package pdf

import "bytes"

// Engine owns page geometry and drives the measure/place passes over a
// Document's widget tree. An Engine is reusable: every Render call builds a
// fresh Renderer (gofpdf accumulates pages, so sharing one across renders
// would leak an earlier document's pages into the next).
type Engine struct {
	// Renderer is replaced with a fresh instance at the start of every
	// Render call; it's exposed so tests and custom Contexts can reach the
	// text-measurement surface without rendering a document.
	Renderer *Renderer

	// fonts is kept so Render can build a fresh Renderer per call.
	fonts FontSet

	PageWidth  float64
	PageHeight float64

	Margin Edges
}

// NewEngine creates an A4-portrait engine with a 10mm margin, embedding
// fonts into its renderer. It returns an error instead of panicking if
// fonts is missing a required variant, so a misconfigured font set fails
// fast at construction rather than during (or after) render.
func NewEngine(fonts FontSet) (*Engine, error) {
	r, err := NewRenderer(fonts)
	if err != nil {
		return nil, err
	}

	return &Engine{
		Renderer: r,
		fonts:    fonts,

		PageWidth:  210,
		PageHeight: 297,

		Margin: All(10),
	}, nil
}

// Render measures and places doc's widget tree, returning the finished PDF
// bytes. It builds a fresh Renderer per call, so the same Engine can render
// multiple documents without pages leaking between them.
func (e *Engine) Render(doc Document) ([]byte, error) {
	r, err := NewRenderer(e.fonts)
	if err != nil {
		return nil, err
	}

	e.Renderer = r

	ctx := &Context{
		Renderer:   e.Renderer,
		PageWidth:  e.PageWidth,
		PageHeight: e.PageHeight,
		Margin:     e.Margin,
		Header:     doc.header,
		Footer:     doc.footer,
	}

	// Measure the header/footer heights once (from page 1) so every page —
	// including the first — reserves the same amount of space, per
	// WithHeader/WithFooter's documented contract.
	ctx.headerHeight = measureChromeHeight(ctx, doc.header, 1)
	ctx.footerHeight = measureChromeHeight(ctx, doc.footer, 1)

	ctx.NewPage()

	// Width is constrained to the page's content width; height is
	// intentionally left unbounded — vertical flow is intrinsic
	// (content-driven), with actual page limits only applied once we know
	// each widget's real size, during Place (see FlexWidget/GridWidget).
	c := Constraints{
		MinW: 0, MaxW: ctx.ContentWidth(),
		MinH: 0, MaxH: Unbounded,
	}

	// A nil root (Page(nil)) renders as an empty page rather than
	// panicking, matching how nil children/headers/footers are tolerated
	// everywhere else in the tree.
	if doc.root != nil {
		placed := doc.root.Measure(ctx, c)
		placed.Place(ctx, ctx.Margin.Left, ctx.PageTop())
	}

	// Surface any error gofpdf accumulated during placement (bad image
	// data, font issues, ...) — gofpdf swallows errors into internal state,
	// so without this check they'd only appear, less attributably, from
	// Output below.
	if err := e.Renderer.Err(); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := e.Renderer.Output(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
