package widget

import (
	"bytes"
	"compress/zlib"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	engine "github.com/wssto2/go-core/pdf"
)

var (
	fontResource = regexp.MustCompile(`/F[0-9a-f]{40}`)
	streamBody   = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
)

// renderPDF renders w on one page.
func renderPDF(t *testing.T, w engine.Widget) []byte {
	t.Helper()

	out, err := engine.Page(Column(w)).PDF(testFonts())
	if err != nil {
		t.Fatalf("PDF() returned error: %v", err)
	}

	return out
}

// drawOps returns the page content stream(s) of a rendered PDF, inflated:
// the drawn operations (rects, lines, glyphs) in order. The whole file is
// not comparable between renders (creation date, and font objects and
// resource names that gofpdf derives in map order), so the font names are
// renumbered by first use, which keeps regular and bold apart.
func drawOps(t *testing.T, pdf []byte) string {
	t.Helper()

	var ops bytes.Buffer

	for _, m := range streamBody.FindAllSubmatch(pdf, -1) {
		r, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			continue // not a deflated stream (font, image)
		}

		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("inflate: %v", err)
		}

		// Page content starts with the "0 J" line-cap setup; fonts and
		// other streams do not.
		if bytes.HasPrefix(b, []byte("0 J")) {
			ops.Write(b)
		}
	}

	seen := map[string]string{}
	named := fontResource.ReplaceAllStringFunc(ops.String(), func(name string) string {
		if _, ok := seen[name]; !ok {
			seen[name] = "/F" + strconv.Itoa(len(seen)+1)
		}

		return seen[name]
	})

	// Text is drawn as 16-bit codes with a zero high byte.
	return strings.ReplaceAll(named, "\x00", "")
}

func measure(t *testing.T, w engine.Widget, maxW float64) engine.Size {
	t.Helper()

	e, err := engine.NewEngine(testFonts())
	if err != nil {
		t.Fatalf("NewEngine() returned error: %v", err)
	}

	ctx := &engine.Context{Renderer: e.Renderer, PageWidth: 210, PageHeight: 297}

	return w.Measure(ctx, engine.Constraints{MaxW: maxW, MaxH: engine.Unbounded}).Size()
}

func count(ops, sub string) int { return strings.Count(ops, sub) }

const (
	lineOp = " l S"  // a stroked line: rules, borders, check marks
	rectOp = " re S" // a stroked rectangle: checkbox outlines
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestSignatureLine(t *testing.T) {
	blank := renderPDF(t, SignatureLine("Sign here", "", 8))
	ops := drawOps(t, blank)

	if count(ops, lineOp) != 1 || !strings.Contains(ops, "(Sign here) Tj") {
		t.Fatalf("blank slot: want one rule and the caption, got ops:\n%s", ops)
	}

	filled := drawOps(t, renderPDF(t, SignatureLine("Sign here", "J. Doe", 8)))
	if count(filled, lineOp) != 1 || !strings.Contains(filled, "(J. Doe) Tj") || !strings.Contains(filled, "(Sign here) Tj") {
		t.Fatalf("filled slot: want one rule, the value and the caption, got ops:\n%s", filled)
	}

	// 8mm room + 0.5mm gap + a one-line 8pt caption; full width.
	got := measure(t, SignatureLine("Sign here", "", 8), 100)
	if want := 8 + 0.5 + 8*ptToMM*defaultLineHeight; !approx(got.H, want) || !approx(got.W, 100) {
		t.Fatalf("size = %+v, want %vx%v", got, 100.0, want)
	}
}

func TestLabelWithValue(t *testing.T) {
	label := measure(t, Text("Name:", FontSize(8)), 100).W

	fixed := measure(t, LabelWithValue("Name:", "x", 20), 100)
	if want := label + 2 + 20; !approx(fixed.W, want) {
		t.Fatalf("fixed width = %v, want %v", fixed.W, want)
	}

	if fill := measure(t, LabelWithValue("Name:", "x", FillWidth), 100); !approx(fill.W, 100) {
		t.Fatalf("FillWidth width = %v, want the whole row (100)", fill.W)
	}

	if natural := measure(t, LabelWithValue("Name:", "x", 0), 100); natural.W >= 100 || natural.W <= fixed.W-20 {
		t.Fatalf("natural width = %v, want the text width", natural.W)
	}

	ops := drawOps(t, renderPDF(t, LabelWithValue("Name:", "Ann", FillWidth)))
	if count(ops, lineOp) != 1 || !strings.Contains(ops, "(Name:) Tj") || !strings.Contains(ops, "(Ann) Tj") {
		t.Fatalf("want one rule, the label and the value, got ops:\n%s", ops)
	}

	// An empty value still draws the rule to write on.
	if empty := drawOps(t, renderPDF(t, LabelWithValue("Name:", "", FillWidth))); count(empty, lineOp) != 1 {
		t.Fatalf("empty value: want the rule, got ops:\n%s", empty)
	}
}

func TestTextLines(t *testing.T) {
	got := measure(t, TextLines(3), 100)
	if !approx(got.W, 100) || !approx(got.H, 3*5.5) {
		t.Fatalf("size = %+v, want 100x16.5", got)
	}

	if rules := count(drawOps(t, renderPDF(t, TextLines(3))), lineOp); rules != 3 {
		t.Fatalf("rules = %d, want 3", rules)
	}
}

func TestCodeBoxes(t *testing.T) {
	got := measure(t, CodeBoxes(4), 100)
	if !approx(got.W, 20) || !approx(got.H, 4) {
		t.Fatalf("size = %+v, want 20x4", got)
	}

	// Each box: right + bottom edge; the first also a left edge.
	if lines := count(drawOps(t, renderPDF(t, CodeBoxes(4))), lineOp); lines != 4*2+1 {
		t.Fatalf("edges = %d, want 9", lines)
	}
}

func TestYesNo(t *testing.T) {
	ops := drawOps(t, renderPDF(t, YesNo(true, false, "Yes", "No")))

	if count(ops, rectOp) != 2 {
		t.Fatalf("checkboxes = %d, want 2", count(ops, rectOp))
	}

	// Only the ticked box gets a check mark (two strokes).
	if count(ops, lineOp) != 2 {
		t.Fatalf("check strokes = %d, want 2", count(ops, lineOp))
	}

	if !strings.Contains(ops, "( Yes) Tj") || !strings.Contains(ops, "( No) Tj") || !strings.Contains(ops, "7.00 Tf") {
		t.Fatalf("want the labels at 7pt, got ops:\n%s", ops)
	}

	if none := drawOps(t, renderPDF(t, YesNo(false, false, "Yes", "No"))); count(none, lineOp) != 0 {
		t.Fatalf("unticked: want no check strokes, got ops:\n%s", none)
	}
}

func TestFieldOptions(t *testing.T) {
	ops := drawOps(t, renderPDF(t, SignatureLine("Sign", "", 8, FieldFontSize(12), FieldRule(1, "#ff0000"))))

	for _, want := range []string{"12.00 Tf", "2.83 w", "1.000 0.000 0.000 RG"} {
		if !strings.Contains(ops, want) {
			t.Fatalf("missing %q in ops:\n%s", want, ops)
		}
	}

	if yn := drawOps(t, renderPDF(t, YesNo(false, false, "Yes", "No", FieldFontSize(10)))); !strings.Contains(yn, "10.00 Tf") {
		t.Fatalf("YesNo FieldFontSize ignored:\n%s", yn)
	}
}

func TestPageBreak(t *testing.T) {
	if got := measure(t, PageBreak(), 100); got != (engine.Size{}) {
		t.Fatalf("size = %+v, want zero", got)
	}

	out := renderPDF(t, Column(Text("first"), PageBreak(), Text("second")))
	if got := pageCount(out); got != 2 {
		t.Fatalf("pages = %d, want 2", got)
	}
}

// TestFormFromAllWidgets renders a small form built from the six widgets.
func TestFormFromAllWidgets(t *testing.T) {
	form := Column(
		Row(
			LabelWithValue("Name:", "Ann Example", FillWidth),
			LabelWithValue("Code:", "", 0),
		).Spacing(4),
		Row(Text("Reference", FontSize(8)), CodeBoxes(8)).Spacing(3),
		Row(Text("Insured?", FontSize(8)), YesNo(true, false, "Yes", "No")).Spacing(3),
		Text("Notes", Bold()),
		TextLines(2),
		Row(
			Column(SignatureLine("Date, signature", "Ann Example", 6)).Modifier(engine.Weight(1)),
			Column(SignatureLine("Date, signature", "", 6)).Modifier(engine.Weight(1)),
		).Spacing(6),
		PageBreak(),
		Text("Terms"),
	)

	out := renderPDF(t, form)
	ops := drawOps(t, out)

	if got := pageCount(out); got != 2 {
		t.Fatalf("pages = %d, want 2 (the PageBreak)", got)
	}

	for _, want := range []string{"(Name:) Tj", "(Ann Example) Tj", "(Reference) Tj", "( Yes) Tj", "(Notes) Tj", "(Date, signature) Tj", "(Terms) Tj"} {
		if !strings.Contains(ops, want) {
			t.Errorf("missing %q in the form", want)
		}
	}
}
