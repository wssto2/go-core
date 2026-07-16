package widget

import (
	"bytes"
	"testing"

	engine "github.com/wssto2/go-core/pdf"
)

// pageCount counts physical pages in a rendered PDF. A plain count of
// "/Type /Page" also matches the document's single page-tree object
// ("/Type /Pages"), so that one is subtracted back out.
func pageCount(out []byte) int {
	return bytes.Count(out, []byte("/Type /Page")) - bytes.Count(out, []byte("/Type /Pages"))
}

// TestAlignInUnboundedSlotReportsChildWidth is a regression test: Align
// used to report its box as c.MaxW verbatim, so inside an unbounded-width
// slot (e.g. a Row's non-flex child) it reported a ~1e12mm width and blew
// up the parent's layout math. It must fall back to the child's own width
// (mirroring the Divider/Image unbounded guards).
func TestAlignInUnboundedSlotReportsChildWidth(t *testing.T) {
	unbounded := engine.Constraints{MinW: 0, MaxW: engine.Unbounded, MinH: 0, MaxH: engine.Unbounded}

	p := Align(Spacer(5), AlignEnd).Measure(nil, unbounded)

	if got, want := p.Size(), (engine.Size{W: 5, H: 5}); got != want {
		t.Fatalf("Align in unbounded slot: got size %+v, want %+v (child's own size)", got, want)
	}

	// A nil child must still be safe and report zero size.
	np := Align(nil, AlignCenter).Measure(nil, unbounded)
	if got := np.Size(); got.W != 0 || got.H != 0 {
		t.Fatalf("Align(nil) in unbounded slot: got size %+v, want zero", got)
	}
}

// TestEngineIsReusableAcrossRenders is a regression test: Engine.Render
// used to reuse one gofpdf instance, so a second Render's output contained
// the first document's pages too.
func TestEngineIsReusableAcrossRenders(t *testing.T) {
	e, err := engine.NewEngine(testFonts())
	if err != nil {
		t.Fatalf("NewEngine() returned error: %v", err)
	}

	first, err := e.Render(engine.Page(Column(Text("doc A"))))
	if err != nil {
		t.Fatalf("first Render() returned error: %v", err)
	}

	second, err := e.Render(engine.Page(Column(Text("doc B"))))
	if err != nil {
		t.Fatalf("second Render() returned error: %v", err)
	}

	pa := pageCount(first)
	pb := pageCount(second)

	if pa != pb {
		t.Fatalf("second Render leaked pages from the first: %d vs %d /Type /Page markers", pb, pa)
	}

	if bytes.Contains(second, []byte("doc A")) {
		t.Fatal("second Render's output contains the first document's content")
	}
}

// TestEngineRendersNilRootAsEmptyPage is a regression test: Page(nil) used
// to panic with a nil pointer dereference inside Render.
func TestEngineRendersNilRootAsEmptyPage(t *testing.T) {
	out, err := engine.Page(nil).PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("output is not a well-formed PDF")
	}

	if got := pageCount(out); got != 1 {
		t.Fatalf("expected exactly 1 empty page, found %d", got)
	}
}

// TestContentContinuesAfterMultiPageChild is a regression test for the flow
// protocol: a sibling placed after a grid that broke across pages used to
// always land on a *fresh* page (the parent Column's cursor went stale the
// moment the child paged internally). With FlowPlaceable, the sibling must
// continue right after the grid on its final page — same total page count
// as the grid alone.
func TestContentContinuesAfterMultiPageChild(t *testing.T) {
	rows := make([][]engine.Widget, 0, 80)
	for range 80 {
		rows = append(rows, GridRow(Text("Row"), Text("Item")))
	}

	gridOnly, err := engine.Page(Column(
		Grid(Columns(Fraction(1), Fraction(1)), rows...),
	)).PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	withTrailer, err := engine.Page(Column(
		Grid(Columns(Fraction(1), Fraction(1)), rows...),
		Text("total due: 100.00"),
	)).PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	gp := pageCount(gridOnly)
	tp := pageCount(withTrailer)

	if gp < 2 {
		t.Fatalf("precondition failed: grid should span multiple pages, got %d", gp)
	}

	if tp != gp {
		t.Fatalf("content after a multi-page grid should continue on its last page: %d pages with trailer, %d without", tp, gp)
	}
}

// TestNestedColumnFlowsWithoutForcedBreak verifies a nested Column that
// overflows the page flows across the boundary (breaking between its own
// children) instead of being pushed wholesale onto a new page by its
// parent — and that a sibling after it continues in sequence.
func TestNestedColumnFlowsWithoutForcedBreak(t *testing.T) {
	inner := make([]engine.Widget, 0, 80)
	for range 80 {
		inner = append(inner, Text("nested line"))
	}

	out, err := engine.Page(Column(
		Text("before"),
		Column(inner...),
		Text("after"),
	)).PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	// 80 lines + 2 surrounding lines fit comfortably on 2 pages; the old
	// behavior produced 3+ (forced break before the nested Column, fresh
	// page after it).
	if got := pageCount(out); got != 2 {
		t.Fatalf("expected nested Column to flow across exactly 2 pages, got %d", got)
	}
}

// TestKeepTogetherReportsFlowEnd verifies KeepTogether participates in the
// flow protocol: content after it continues on the same page (no forced
// fresh page), while the block itself still moves to a new page as a unit
// when it doesn't fit.
func TestKeepTogetherReportsFlowEnd(t *testing.T) {
	filler := make([]engine.Widget, 0, 60)
	for range 60 {
		filler = append(filler, Text("filler line to consume most of page one"))
	}

	out, err := engine.Page(Column(
		Column(filler...),
		KeepTogether(
			Text("kept line 1"),
			Text("kept line 2"),
		),
		Text("after the kept block"),
	)).PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	// Filler ~ page 1, kept block pushed to page 2, trailer right after it
	// on page 2 — not on a page of its own.
	if got := pageCount(out); got != 2 {
		t.Fatalf("expected 2 pages (trailer continues after KeepTogether), got %d", got)
	}
}

// TestTextLineHeightOption verifies the LineHeight option scales the
// measured height (default multiplier is 1.4).
func TestTextLineHeightOption(t *testing.T) {
	e, err := engine.NewEngine(testFonts())
	if err != nil {
		t.Fatalf("NewEngine() returned error: %v", err)
	}

	ctx := &engine.Context{Renderer: e.Renderer}
	c := engine.Constraints{MinW: 0, MaxW: 100, MinH: 0, MaxH: engine.Unbounded}

	def := Text("hello").Measure(ctx, c).Size().H
	tight := Text("hello", LineHeight(1.0)).Measure(ctx, c).Size().H

	if tight >= def {
		t.Fatalf("LineHeight(1.0) height %v should be smaller than default (1.4) height %v", tight, def)
	}

	want := def / 1.4
	if diff := tight - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("LineHeight(1.0) height = %v, want %v (default/1.4)", tight, want)
	}
}
