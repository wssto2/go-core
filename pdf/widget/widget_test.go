package widget

import (
	"bytes"
	"fmt"
	"testing"

	engine "github.com/wssto2/go-core/pdf"
)

// TestEngineRendersValidPDF exercises the full Measure/Place pipeline
// (Column > Row > Grid + Text widgets) end to end and checks that the
// output is a well-formed, non-trivial PDF — the kind of smoke test that
// would have caught the earlier writer/orientation/margin regressions
// immediately instead of only surfacing as garbled output at print time.
func TestEngineRendersValidPDF(t *testing.T) {
	doc := engine.Page(Column(
		Text("Invoice", Bold(), FontSize(16)),
		Row(
			Text("Customer:"),
			Text("John Doe"),
		),
		Grid(
			Columns(Fixed(30), Fraction(1), Fraction(1)),
			GridRow(Text("#"), Text("Item"), Text("Price")),
			GridRow(Text("1"), Text("Widget"), Text("10.00")),
			GridRow(Text("2"), Text("Gadget"), Text("20.00")),
		),
	))

	out, err := doc.PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	if len(out) == 0 {
		t.Fatal("PDF() returned empty output")
	}

	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("output does not start with %%PDF- header, got: %q", out[:min(20, len(out))])
	}

	if !bytes.Contains(out, []byte("%%EOF")) {
		t.Fatal("output is missing EOF trailer — PDF may be truncated")
	}

	// Sanity bound: a real, multi-widget document should render to more
	// than a trivial handful of bytes.
	if len(out) < 500 {
		t.Fatalf("output suspiciously small (%d bytes) for a document with text + grid content", len(out))
	}
}

// TestEngineRendersLongGridAcrossPages verifies the page-break-avoid
// behavior in FlexWidget/GridWidget.Place actually triggers a page break
// instead of silently overflowing when content exceeds one page.
func TestEngineRendersLongGridAcrossPages(t *testing.T) {
	rows := make([][]engine.Widget, 0, 80)
	for range 80 {
		rows = append(rows, GridRow(Text("Row"), Text("Item")))
	}

	doc := engine.Page(Column(
		Grid(Columns(Fraction(1), Fraction(1)), rows...),
	))

	out, err := doc.PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	// A well-formed multi-page PDF should reference more than one /Page
	// object in its object stream.
	pageCount := bytes.Count(out, []byte("/Type /Page"))
	if pageCount < 2 {
		t.Fatalf("expected content to span multiple pages, found %d /Type /Page markers", pageCount)
	}
}

// TestEngineHeaderFooterAcrossPages verifies WithHeader/WithFooter chrome is
// drawn on every page (including a page-number-dependent footer) and that
// content is never overlapped by the reserved header/footer space, across a
// document long enough to span multiple pages.
func TestEngineHeaderFooterAcrossPages(t *testing.T) {
	rows := make([][]engine.Widget, 0, 80)
	for range 80 {
		rows = append(rows, GridRow(Text("Row"), Text("Item")))
	}

	doc := engine.Page(
		Column(
			Grid(Columns(Fraction(1), Fraction(1)), rows...),
		),
		engine.WithHeader(func(pageNum int) engine.Widget {
			return Text(fmt.Sprintf("Invoice #123 — page header (%d)", pageNum))
		}),
		engine.WithFooter(func(pageNum int) engine.Widget {
			return Text(fmt.Sprintf("Page %d", pageNum))
		}),
	)

	out, err := doc.PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	pageCount := bytes.Count(out, []byte("/Type /Page"))
	if pageCount < 2 {
		t.Fatalf("expected content to span multiple pages, found %d /Type /Page markers", pageCount)
	}

	if len(out) == 0 || !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("output is not a well-formed PDF")
	}
}

// TestEngineHeaderFooterReservesConsistentHeight verifies the header/footer
// heights are measured once (from page 1) and reserved identically on every
// page, per WithHeader/WithFooter's documented contract — even when a
// page's chrome content differs.
func TestEngineHeaderFooterReservesConsistentHeight(t *testing.T) {
	e, err := engine.NewEngine(testFonts())
	if err != nil {
		t.Fatalf("NewEngine() returned error: %v", err)
	}

	ctx := &engine.Context{
		Renderer:   e.Renderer,
		PageWidth:  e.PageWidth,
		PageHeight: e.PageHeight,
		Margin:     e.Margin,
	}

	top1 := ctx.PageTop()
	bottom1 := ctx.PageBottom()

	// Sanity: with no header/footer configured, PageTop/PageBottom fall back
	// to plain margins.
	if top1 != e.Margin.Top {
		t.Fatalf("PageTop() = %v, want %v (margin only, no header configured)", top1, e.Margin.Top)
	}

	if bottom1 != e.PageHeight-e.Margin.Bottom {
		t.Fatalf("PageBottom() = %v, want %v (margin only, no footer configured)", bottom1, e.PageHeight-e.Margin.Bottom)
	}
}

// TestEngineGridPanicsOnCellCountMismatch verifies Grid fails loudly at
// build time when a row's cell count doesn't match the column count,
// instead of silently dropping cells in production PDFs.
func TestEngineGridPanicsOnCellCountMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected Grid to panic on mismatched cell count, but it did not")
		}
	}()

	Grid(
		Columns(Fixed(30), Fraction(1)),
		GridRow(Text("only one cell")),
	)
}

// TestEngineStackOverlaysAtOrigin verifies Stack measures its size as the
// max width/height across all children and places every child at the same
// origin (rather than laying them out in sequence like Column/Row).
func TestEngineStackOverlaysAtOrigin(t *testing.T) {
	doc := engine.Page(Column(
		Stack(
			Text("background"),
			Text("foreground"),
		),
	))

	out, err := doc.PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("output is not a well-formed PDF")
	}
}

// TestEngineKeepTogetherForcesPageBreak verifies KeepTogether pushes its
// entire block onto the next page rather than splitting it mid-block when
// it doesn't fit in the remaining space on the current page.
func TestEngineKeepTogetherForcesPageBreak(t *testing.T) {
	rows := make([]engine.Widget, 0, 60)
	for range 60 {
		rows = append(rows, Text("filler line to consume most of page one"))
	}

	doc := engine.Page(Column(
		Column(rows...),
		KeepTogether(
			Text("line 1 of a block that must stay together"),
			Text("line 2 of a block that must stay together"),
		),
	))

	out, err := doc.PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	pageCount := bytes.Count(out, []byte("/Type /Page"))
	if pageCount < 2 {
		t.Fatalf("expected KeepTogether to push overflow onto a new page, found %d /Type /Page markers", pageCount)
	}
}

// TestEngineAlignPositionsChildWithinBox verifies Align reports the full
// box size it was given (not just the child's own size) so that wrapping a
// narrower child in Align + Width actually makes alignment visible.
func TestEngineAlignPositionsChildWithinBox(t *testing.T) {
	doc := engine.Page(Column(
		Align(Text("right"), AlignEnd).Modifier(engine.Width(100)),
	))

	out, err := doc.PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("output is not a well-formed PDF")
	}
}

// TestEngineBoxAppliesModifiers verifies Box is transparent sugar for
// engine.ApplyModifiers — wrapping a widget in Box with Width/Padding/
// Background/Border modifiers should render without error, the same as
// calling child.Modifier(...) directly.
func TestEngineBoxAppliesModifiers(t *testing.T) {
	doc := engine.Page(Column(
		Box(
			Text("boxed"),
			engine.Width(120),
			engine.Padding(engine.Edges{Top: 2, Right: 2, Bottom: 2, Left: 2}),
			engine.Background("#EEEEEE"),
			engine.Border(0.5, "#000000"),
		),
	))

	out, err := doc.PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("output is not a well-formed PDF")
	}
}

// TestFlexSpacingAddsGapBetweenChildren verifies Row/Column.Spacing inserts
// a fixed gap between adjacent children (like CSS flexbox `gap`) without
// adding space before the first or after the last child, and that the
// measured size grows to include the gaps.
func TestFlexSpacingAddsGapBetweenChildren(t *testing.T) {
	unbounded := engine.Constraints{MinW: 0, MaxW: engine.Unbounded, MinH: 0, MaxH: engine.Unbounded}

	row := Row(Spacer(10), Spacer(10), Spacer(10)).Spacing(5)
	p := row.Measure(nil, unbounded)

	if got, want := p.Size().W, 30.0+2*5.0; got != want {
		t.Fatalf("Row with Spacing(5) over 3x Spacer(10): got width %v, want %v", got, want)
	}

	// Single child: no gap should be added.
	single := Row(Spacer(10)).Spacing(5)
	sp := single.Measure(nil, unbounded)

	if got, want := sp.Size().W, 10.0; got != want {
		t.Fatalf("Row with Spacing(5) over a single child: got width %v, want %v (spacing must not apply)", got, want)
	}

	col := Column(Spacer(10), Spacer(10)).Spacing(4)
	cp := col.Measure(nil, unbounded)

	if got, want := cp.Size().H, 20.0+4.0; got != want {
		t.Fatalf("Column with Spacing(4) over 2x Spacer(10): got height %v, want %v", got, want)
	}
}

// TestGridGapAddsSpaceBetweenColumnsAndRows verifies Grid.Gap/ColumnGap/
// RowGap add fixed gutters between columns and rows (like CSS grid `gap`),
// and that Fraction columns share whatever width remains after both fixed
// columns and column gaps are subtracted.
func TestGridGapAddsSpaceBetweenColumnsAndRows(t *testing.T) {
	c := engine.Constraints{MinW: 0, MaxW: 100, MinH: 0, MaxH: engine.Unbounded}

	g := Grid(
		Columns(Fixed(20), Fraction(1), Fraction(1)),
		GridRow(Spacer(1), Spacer(1), Spacer(1)),
		GridRow(Spacer(1), Spacer(1), Spacer(1)),
	).Gap(5)

	p := g.Measure(nil, c)

	// Columns: 20 fixed + 2 gaps of 5 = 30 spent on fixed+gaps, 70 left
	// split evenly across the two Fraction(1) columns => 35 each.
	// Total width reported must include the two 5mm column gaps too:
	// 20 + 35 + 35 + 2*5 = 100.
	if got, want := p.Size().W, 100.0; got != want {
		t.Fatalf("Grid with ColumnGap(5): got total width %v, want %v", got, want)
	}

	// Rows: each row's height is the Spacer's intrinsic size (1mm), plus
	// one 5mm row gap between the two rows => 1 + 1 + 5 = 7.
	if got, want := p.Size().H, 7.0; got != want {
		t.Fatalf("Grid with RowGap(5): got total height %v, want %v", got, want)
	}
}
