//nolint:testpackage // shares testFonts and the helpers of form_test.go
package widget

import (
	"testing"

	engine "github.com/wssto2/go-core/pdf"
)

// The reference* functions below are the form helpers as the application
// they were extracted from had them, copied verbatim from its widget kit
// (only the Yes/No caption literals were changed to English, since the
// library takes them as parameters, and
// the bodies use this package's unqualified names). The
// tests render each reference and its library counterpart with the same
// engine and require identical page content (every drawn rule, rectangle
// and text run, in order, at the same coordinates) and page count. The
// complete PDF files cannot be compared byte for byte: gofpdf writes a
// creation date and orders font objects by map iteration, so even two
// renders of one widget differ. Page content is where the widgets act.

func referenceTextLines(number int) *FlexWidget {
	lines := make([]engine.Widget, 0, number)

	for range number {
		lines = append(lines,
			Row(
				Column().Modifier(engine.Weight(1)),
			).Modifier(
				engine.BorderBottom(0.2, "#000"),
				engine.Height(5.5),
			),
		)
	}

	return Column(lines...)
}

func referenceSignatureLine(caption, value string, spaceAboveLine float64) *FlexWidget {
	filler := Row(
		Column().Modifier(engine.Weight(1)),
	)

	slot := []engine.Widget{
		Spacer(spaceAboveLine),
	}

	if value != "" {
		slot = append(slot,
			Row(
				Text(value, FontSize(8)).
					Modifier(engine.Padding(engine.Edges{Top: 0, Right: 0, Bottom: 1, Left: 2})),
				Column().Modifier(engine.Weight(1)),
			),
		)
	} else {
		slot = append(slot, filler)
	}

	return Column(
		Column(slot...).Modifier(engine.BorderBottom(0.2, "#000")),
		Spacer(0.5),
		Text(caption, FontSize(8)),
	)
}

func referenceLabelWithValue(label string, value string, width float64) *FlexWidget {
	valueModifiers := []engine.Modifier{}

	if value == "" {
		value = " "
	}

	if width > 0 {
		valueModifiers = append(valueModifiers, engine.Width(width))
	}

	if width == -1 {
		valueModifiers = append(valueModifiers, engine.Weight(1))
	}

	valueModifiers = append(valueModifiers, engine.BorderBottom(0.2, "#000"),
		engine.Padding(engine.Edges{
			Top:    0,
			Bottom: 0.3,
			Left:   0,
			Right:  2,
		}))

	return Row(
		Text(label, FontSize(8)).
			Modifier(
				engine.Padding(engine.Edges{Top: 0, Right: 2, Bottom: 0, Left: 0}),
			),

		Text(value, FontSize(8), Bold()).
			Modifier(valueModifiers...),
	)
}

func referenceCodeBoxes(length int) *FlexWidget {
	codeBoxes := make([]engine.Widget, 0, length)

	for i := range length {
		borderLeftWidth := 0.0
		if i == 0 {
			borderLeftWidth = 0.2
		}

		codeBoxes = append(codeBoxes, Text("").
			Modifier(
				engine.Width(5),
				engine.Height(4),
				engine.BorderLeft(borderLeftWidth, "#000"),
				engine.BorderRight(0.2, "#000"),
				engine.BorderBottom(0.2, "#000"),
			),
		)
	}

	return Row(codeBoxes...)
}

func referenceYesNo(yesChecked bool, noChecked bool) *FlexWidget {
	return Row(
		Checkbox(yesChecked).Size(3.5),
		Text(" YES", FontSize(7)).Modifier(engine.Padding(engine.Edges{Top: 0, Right: 3, Bottom: 0, Left: 0.5})),
		Checkbox(noChecked).Size(3.5),
		Text(" NO", FontSize(7)).Modifier(engine.Padding(engine.Edges{Top: 0, Right: 0, Bottom: 0, Left: 0.5})),
	)
}

type referencePageBreak struct{}

//nolint:ireturn // the Widget interface fixes the return type
func (referencePageBreak) Measure(*engine.Context, engine.Constraints) engine.Placeable {
	return referencePageBreakPlaceable{}
}

type referencePageBreakPlaceable struct{}

func (referencePageBreakPlaceable) Size() engine.Size { return engine.Size{W: 0, H: 0} }

func (p referencePageBreakPlaceable) Place(ctx *engine.Context, x, y float64) {
	p.PlaceFlow(ctx, x, y)
}

func (referencePageBreakPlaceable) PlaceFlow(ctx *engine.Context, _, _ float64) float64 {
	ctx.NewPage()

	return ctx.PageTop()
}

func TestMatchesReferenceImplementations(t *testing.T) {
	t.Parallel()

	// Each case sits in a Column with a text line around it, as it would in
	// a document, so the surrounding flow is compared too.
	cases := []struct {
		name      string
		reference engine.Widget
		library   engine.Widget
	}{
		{"text lines", referenceTextLines(2), TextLines(2)},
		{"text lines none", referenceTextLines(0), TextLines(0)},
		{"signature blank", referenceSignatureLine("Sign here", "", 8), SignatureLine("Sign here", "", 8)},
		{"signature filled", referenceSignatureLine("Sign here", "J. Doe", 6), SignatureLine("Sign here", "J. Doe", 6)},
		{"label fill", referenceLabelWithValue("Name:", "Ann", -1), LabelWithValue("Name:", "Ann", FillWidth)},
		{"label fixed", referenceLabelWithValue("Hour:", "12", 20), LabelWithValue("Hour:", "12", 20)},
		{"label natural", referenceLabelWithValue("Note:", "x", 0), LabelWithValue("Note:", "x", 0)},
		{"label empty", referenceLabelWithValue("Name:", "", -1), LabelWithValue("Name:", "", FillWidth)},
		{"code boxes", referenceCodeBoxes(7), CodeBoxes(7)},
		{"yes no unticked", referenceYesNo(false, false), YesNo(false, false, "YES", "NO")},
		{"yes ticked", referenceYesNo(true, false), YesNo(true, false, "YES", "NO")},
		{"no ticked", referenceYesNo(false, true), YesNo(false, true, "YES", "NO")},
		{"page break", referencePageBreak{}, PageBreak()},
		{
			"signature pair",
			Row(
				Column(referenceSignatureLine("Buyer", "Ann", 6)).Modifier(engine.Weight(1)),
				Column(referenceSignatureLine("Seller", "", 6)).Modifier(engine.Weight(1)),
			).Spacing(6),
			Row(
				Column(SignatureLine("Buyer", "Ann", 6)).Modifier(engine.Weight(1)),
				Column(SignatureLine("Seller", "", 6)).Modifier(engine.Weight(1)),
			).Spacing(6),
		},
	}

	wrap := func(w engine.Widget) engine.Widget {
		return Column(Text("before"), w, Text("after"))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ref := renderPDF(t, wrap(tc.reference))
			got := renderPDF(t, wrap(tc.library))

			refOps, gotOps := drawOps(t, ref), drawOps(t, got)

			if refOps == "" || refOps != gotOps || pageCount(ref) != pageCount(got) {
				t.Fatalf("page content differs\nwant (reference):\n%s\ngot (library):\n%s", refOps, gotOps)
			}
		})
	}
}
