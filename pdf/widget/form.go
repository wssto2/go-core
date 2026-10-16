package widget

import engine "github.com/wssto2/go-core/pdf"

// FillWidth, passed as the width of LabelWithValue, makes the value slot
// take all the space left in its row (Weight 1).
const FillWidth = -1.0

// Form-field defaults. They describe a compact paper-form look: 8pt text,
// 0.2mm black rules, 7pt Yes/No captions.
const (
	fieldFontSize = 8.0
	yesNoFontSize = 7.0
	fieldRuleMM   = 0.2
	fieldRuleHex  = "#000"
)

type fieldStyle struct {
	fontSize  float64
	ruleWidth float64
	ruleColor string
}

// FieldOption restyles a form widget (SignatureLine, LabelWithValue,
// TextLines, CodeBoxes, YesNo). An option a widget has no use for is
// ignored: FieldRule does nothing on YesNo, FieldFontSize nothing on
// TextLines and CodeBoxes.
type FieldOption func(*fieldStyle)

// FieldFontSize sets the text size in points (default 8; 7 for the YesNo
// captions).
func FieldFontSize(pt float64) FieldOption {
	return func(s *fieldStyle) { s.fontSize = pt }
}

// FieldRule sets the width (mm) and hex colour of the rules and box borders
// (default 0.2mm, black).
func FieldRule(widthMM float64, hex string) FieldOption {
	return func(s *fieldStyle) { s.ruleWidth, s.ruleColor = widthMM, hex }
}

func newFieldStyle(fontSize float64, opts []FieldOption) fieldStyle {
	s := fieldStyle{fontSize: fontSize, ruleWidth: fieldRuleMM, ruleColor: fieldRuleHex}

	for _, o := range opts {
		o(&s)
	}

	return s
}

// fillRow is an empty weighted Row: an empty Column is zero-width, but
// weighted inside a Row (whose width is bounded by its parent) it
// stretches to the full width, which makes the row report that width.
func fillRow() *FlexWidget {
	return Row(Column().Modifier(engine.Weight(1)))
}

// SignatureLine builds a signature slot: spaceAbove mm of writing room, a
// rule across the full width, and the caption under it. A non-empty value
// is printed on the rule (pre-filled signature line).
func SignatureLine(caption, value string, spaceAbove float64, opts ...FieldOption) *FlexWidget {
	s := newFieldStyle(fieldFontSize, opts)

	slot := []engine.Widget{Spacer(spaceAbove)}

	if value != "" {
		slot = append(slot, Row(
			Text(value, FontSize(s.fontSize)).Modifier(engine.Padding(engine.Edges{Bottom: 1, Left: 2})),
			Column().Modifier(engine.Weight(1)),
		))
	} else {
		slot = append(slot, fillRow())
	}

	return Column(
		Column(slot...).Modifier(engine.BorderBottom(s.ruleWidth, s.ruleColor)),
		Spacer(0.5),
		Text(caption, FontSize(s.fontSize)),
	)
}

// LabelWithValue builds "label  value" with the bold value on a rule. width
// is the value slot's width in mm; 0 sizes it to the text, FillWidth to the
// rest of the row. An empty value leaves a blank rule to write on.
func LabelWithValue(label, value string, width float64, opts ...FieldOption) *FlexWidget {
	s := newFieldStyle(fieldFontSize, opts)

	if value == "" {
		value = " "
	}

	var mods []engine.Modifier

	if width > 0 {
		mods = append(mods, engine.Width(width))
	}

	if width == FillWidth {
		mods = append(mods, engine.Weight(1))
	}

	mods = append(mods,
		engine.BorderBottom(s.ruleWidth, s.ruleColor),
		engine.Padding(engine.Edges{Bottom: 0.3, Right: 2}),
	)

	return Row(
		Text(label, FontSize(s.fontSize)).Modifier(engine.Padding(engine.Edges{Right: 2})),
		Text(value, FontSize(s.fontSize), Bold()).Modifier(mods...),
	)
}

// TextLines builds n ruled writing lines, 5.5mm apart, full width.
func TextLines(n int, opts ...FieldOption) *FlexWidget {
	s := newFieldStyle(fieldFontSize, opts)
	lines := make([]engine.Widget, 0, n)

	for range n {
		lines = append(lines, fillRow().Modifier(
			engine.BorderBottom(s.ruleWidth, s.ruleColor),
			engine.Height(5.5), // room to write in, the rule at its bottom
		))
	}

	return Column(lines...)
}

// CodeBoxes builds a row of n comb boxes (5x4mm) for writing one character
// each, e.g. a code or number.
func CodeBoxes(n int, opts ...FieldOption) *FlexWidget {
	s := newFieldStyle(fieldFontSize, opts)
	boxes := make([]engine.Widget, 0, n)

	for i := range n {
		leftWidth := 0.0
		if i == 0 {
			leftWidth = s.ruleWidth
		}

		boxes = append(boxes, Text("").Modifier(
			engine.Width(5),
			engine.Height(4),
			engine.BorderLeft(leftWidth, s.ruleColor),
			engine.BorderRight(s.ruleWidth, s.ruleColor),
			engine.BorderBottom(s.ruleWidth, s.ruleColor),
		))
	}

	return Row(boxes...)
}

// YesNo builds two labelled checkboxes, e.g. YesNo(true, false, "Yes",
// "No"). The labels are the caller's; a space is put in front of each.
func YesNo(yes, no bool, yesLabel, noLabel string, opts ...FieldOption) *FlexWidget {
	s := newFieldStyle(yesNoFontSize, opts)

	return Row(
		Checkbox(yes).Size(3.5),
		Text(" "+yesLabel, FontSize(s.fontSize)).Modifier(engine.Padding(engine.Edges{Left: 0.5, Right: 3})),
		Checkbox(no).Size(3.5),
		Text(" "+noLabel, FontSize(s.fontSize)).Modifier(engine.Padding(engine.Edges{Left: 0.5})),
	)
}

// PageBreak starts a new page: the Column it sits in continues at the top
// of the next one.
func PageBreak() engine.Widget { return pageBreak{} }

type pageBreak struct{}

func (pageBreak) Measure(*engine.Context, engine.Constraints) engine.Placeable {
	return pageBreakPlaceable{}
}

type pageBreakPlaceable struct{}

func (pageBreakPlaceable) Size() engine.Size { return engine.Size{} }

func (p pageBreakPlaceable) Place(ctx *engine.Context, x, y float64) { p.PlaceFlow(ctx, x, y) }

func (pageBreakPlaceable) PlaceFlow(ctx *engine.Context, _, _ float64) float64 {
	ctx.NewPage()

	return ctx.PageTop()
}
