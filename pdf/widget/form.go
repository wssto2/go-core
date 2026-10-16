package widget

import engine "github.com/wssto2/go-core/pdf"

// FillWidth is the width to pass to LabelWithValue for a value slot that
// takes all the space left in its row (Weight 1).
const FillWidth = -1.0

// Form-field defaults. They describe a compact paper-form look: 8pt text,
// 0.2mm black rules, 7pt Yes/No captions.
const (
	fieldFontSize = 8.0
	yesNoFontSize = 7.0
	fieldRuleMM   = 0.2
	fieldRuleHex  = "#000"

	signatureGapMM  = 0.5 // between a signature rule and its caption
	writingLineMM   = 5.5 // pitch of TextLines
	codeBoxWidthMM  = 5.0
	codeBoxHeightMM = 4.0
	checkboxMM      = 3.5

	valueInsetMM   = 2.0 // after a label; before a signature value; after a value
	valueLiftMM    = 0.3 // a value above its rule
	signedLiftMM   = 1.0 // a signature value above its rule
	yesGapMM       = 3.0 // after a Yes caption
	captionNudgeMM = 0.5 // between a checkbox and its caption
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
	return func(style *fieldStyle) { style.fontSize = pt }
}

// FieldRule sets the width (mm) and hex colour of the rules and box borders
// (default 0.2mm, black).
func FieldRule(widthMM float64, hex string) FieldOption {
	return func(style *fieldStyle) { style.ruleWidth, style.ruleColor = widthMM, hex }
}

func newFieldStyle(fontSize float64, opts []FieldOption) fieldStyle {
	style := fieldStyle{fontSize: fontSize, ruleWidth: fieldRuleMM, ruleColor: fieldRuleHex}

	for _, opt := range opts {
		opt(&style)
	}

	return style
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
	style := newFieldStyle(fieldFontSize, opts)

	slot := []engine.Widget{Spacer(spaceAbove)}

	if value != "" {
		slot = append(slot, Row(
			Text(value, FontSize(style.fontSize)).Modifier(
				engine.Padding(engine.Edges{Top: 0, Right: 0, Bottom: signedLiftMM, Left: valueInsetMM}),
			),
			Column().Modifier(engine.Weight(1)),
		))
	} else {
		slot = append(slot, fillRow())
	}

	return Column(
		Column(slot...).Modifier(engine.BorderBottom(style.ruleWidth, style.ruleColor)),
		Spacer(signatureGapMM),
		Text(caption, FontSize(style.fontSize)),
	)
}

// LabelWithValue builds "label  value" with the bold value on a rule. width
// is the value slot's width in mm; 0 sizes it to the text, FillWidth to the
// rest of the row. An empty value leaves a blank rule to write on.
func LabelWithValue(label, value string, width float64, opts ...FieldOption) *FlexWidget {
	style := newFieldStyle(fieldFontSize, opts)

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
		engine.BorderBottom(style.ruleWidth, style.ruleColor),
		engine.Padding(engine.Edges{Top: 0, Right: valueInsetMM, Bottom: valueLiftMM, Left: 0}),
	)

	return Row(
		Text(label, FontSize(style.fontSize)).Modifier(
			engine.Padding(engine.Edges{Top: 0, Right: valueInsetMM, Bottom: 0, Left: 0}),
		),
		Text(value, FontSize(style.fontSize), Bold()).Modifier(mods...),
	)
}

// TextLines builds n ruled writing lines, 5.5mm apart, full width.
func TextLines(n int, opts ...FieldOption) *FlexWidget {
	style := newFieldStyle(fieldFontSize, opts)
	lines := make([]engine.Widget, 0, n)

	for range n {
		lines = append(lines, fillRow().Modifier(
			engine.BorderBottom(style.ruleWidth, style.ruleColor),
			engine.Height(writingLineMM), // room to write in, the rule at its bottom
		))
	}

	return Column(lines...)
}

// CodeBoxes builds a row of n comb boxes (5x4mm) for writing one character
// each, e.g. a code or number.
func CodeBoxes(n int, opts ...FieldOption) *FlexWidget {
	style := newFieldStyle(fieldFontSize, opts)
	boxes := make([]engine.Widget, 0, n)

	for i := range n {
		leftWidth := 0.0
		if i == 0 {
			leftWidth = style.ruleWidth
		}

		boxes = append(boxes, Text("").Modifier(
			engine.Width(codeBoxWidthMM),
			engine.Height(codeBoxHeightMM),
			engine.BorderLeft(leftWidth, style.ruleColor),
			engine.BorderRight(style.ruleWidth, style.ruleColor),
			engine.BorderBottom(style.ruleWidth, style.ruleColor),
		))
	}

	return Row(boxes...)
}

// YesNo builds two labelled checkboxes, e.g. YesNo(true, false, "Yes",
// "No"). The labels are the caller's; a space is put in front of each.
func YesNo(yesChecked, noChecked bool, yesLabel, noLabel string, opts ...FieldOption) *FlexWidget {
	style := newFieldStyle(yesNoFontSize, opts)

	return Row(
		Checkbox(yesChecked).Size(checkboxMM),
		Text(" "+yesLabel, FontSize(style.fontSize)).Modifier(
			engine.Padding(engine.Edges{Top: 0, Right: yesGapMM, Bottom: 0, Left: captionNudgeMM}),
		),
		Checkbox(noChecked).Size(checkboxMM),
		Text(" "+noLabel, FontSize(style.fontSize)).Modifier(
			engine.Padding(engine.Edges{Top: 0, Right: 0, Bottom: 0, Left: captionNudgeMM}),
		),
	)
}

// PageBreakWidget starts a new page when placed.
type PageBreakWidget struct{}

// PageBreak starts a new page: the Column it sits in continues at the top
// of the next one.
func PageBreak() *PageBreakWidget { return &PageBreakWidget{} }

// Measure implements engine.Widget.
//
//nolint:ireturn // the Widget interface fixes the return type
func (*PageBreakWidget) Measure(*engine.Context, engine.Constraints) engine.Placeable {
	return pageBreakPlaceable{}
}

type pageBreakPlaceable struct{}

func (pageBreakPlaceable) Size() engine.Size { return engine.Size{W: 0, H: 0} }

func (p pageBreakPlaceable) Place(ctx *engine.Context, x, y float64) { p.PlaceFlow(ctx, x, y) }

func (pageBreakPlaceable) PlaceFlow(ctx *engine.Context, _, _ float64) float64 {
	ctx.NewPage()

	return ctx.PageTop()
}
