package pdf

// Document is a single-page-flow document built from a widget tree, with
// optional per-page header/footer chrome.
type Document struct {
	root Widget

	header func(pageNum int) Widget
	footer func(pageNum int) Widget
}

// PageOption configures optional document-level chrome (header/footer) on a
// Document, applied by Page.
type PageOption func(*Document)

// WithHeader sets a builder invoked once per page (given the page's 1-based
// number) to produce a widget rendered at the top of every page, above the
// normal content flow. The header's height is measured once, from page 1,
// and that height is reserved on every page — vary the header's *content*
// per page freely (e.g. embed the page number), but keep its height
// consistent across pages, since later pages reuse the page-1 measurement
// rather than re-measuring.
func WithHeader(fn func(pageNum int) Widget) PageOption {
	return func(d *Document) { d.header = fn }
}

// WithFooter mirrors WithHeader but renders at the bottom of every page.
func WithFooter(fn func(pageNum int) Widget) PageOption {
	return func(d *Document) { d.footer = fn }
}

// Page builds a Document from a root widget, with optional header/footer
// PageOptions. The root is typically a Column: vertical flow is the axis
// that knows how to break across pages (see FlexWidget/GridWidget.Place) —
// put page-breakable content at the top level rather than nested inside a
// Row.
func Page(w Widget, opts ...PageOption) Document {
	d := Document{root: w}

	for _, opt := range opts {
		opt(&d)
	}

	return d
}

// PDF renders the document with the given fonts and returns the raw PDF
// bytes. See FontSet/NewEngine for the constraints on fonts.
func (d Document) PDF(fonts FontSet) ([]byte, error) {
	e, err := NewEngine(fonts)
	if err != nil {
		return nil, err
	}

	return e.Render(d)
}
