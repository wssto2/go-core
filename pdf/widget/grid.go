package widget

import (
	"fmt"
	"math"

	engine "github.com/wssto2/go-core/pdf"
)

type widthKind int

const (
	widthFixed widthKind = iota
	widthFraction
)

// ColumnSpec describes one Grid column's width: either a fixed size (mm) or
// a fraction of whatever width is left after fixed columns are subtracted
// (like a CSS grid `fr` unit).
type ColumnSpec struct {
	kind  widthKind
	value float64
}

// Fixed declares a column with a fixed width in mm.
func Fixed(w float64) ColumnSpec { return ColumnSpec{kind: widthFixed, value: w} }

// Fraction declares a column that shares the remaining width proportionally
// to its value, after all Fixed columns are subtracted.
func Fraction(f float64) ColumnSpec { return ColumnSpec{kind: widthFraction, value: f} }

// Columns is a small readability helper for building a Grid's column list.
func Columns(specs ...ColumnSpec) []ColumnSpec { return specs }

// GridRow is a small readability helper for building one row's cells.
func GridRow(cells ...engine.Widget) []engine.Widget { return cells }

// GridWidget is a simple fixed-column-count table: every row is expected to
// provide one cell per column. It's the practical "just give me a table"
// building block for invoice line items etc. — not a full CSS-grid (no
// spanning cells, no auto-placement).
type GridWidget struct {
	columns    []ColumnSpec
	rows       [][]engine.Widget
	headerRows int
	columnGap  float64
	rowGap     float64
}

// RepeatHeader marks the first n rows as a repeating header: whenever the
// table breaks across a page, these rows are re-placed at the top of every
// subsequent page before the remaining body rows continue.
func (g *GridWidget) RepeatHeader(n int) *GridWidget {
	g.headerRows = n
	return g
}

// Grid builds a table with the given column layout and rows (see Columns/
// GridRow). Panics if any row's cell count doesn't match len(columns) —
// fail loud at build time rather than silently drop data in production
// PDFs.
func Grid(columns []ColumnSpec, rows ...[]engine.Widget) *GridWidget {
	for ri, row := range rows {
		if len(row) != len(columns) {
			panic(fmt.Sprintf("engine: grid row %d has %d cells, want %d", ri, len(row), len(columns)))
		}
	}

	return &GridWidget{columns: columns, rows: rows}
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (g *GridWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(g, mods)
}

// Gap sets a uniform gap (mm) between both columns and rows — the CSS grid
// `gap` shorthand. Equivalent to ColumnGap(v).RowGap(v).
func (g *GridWidget) Gap(v float64) *GridWidget {
	g.columnGap = v
	g.rowGap = v
	return g
}

// ColumnGap sets a fixed horizontal gap (mm) inserted between adjacent
// columns. Fraction columns share whatever width remains after gaps (like
// fixed columns) are subtracted.
func (g *GridWidget) ColumnGap(v float64) *GridWidget {
	g.columnGap = v
	return g
}

// RowGap sets a fixed vertical gap (mm) inserted between adjacent rows.
func (g *GridWidget) RowGap(v float64) *GridWidget {
	g.rowGap = v
	return g
}

func (g *GridWidget) resolveColumnWidths(available float64) []float64 {
	// An unbounded available width (e.g. Grid nested in a Row's non-flex
	// slot) would otherwise blow Fraction columns up to ~1e12mm; treat it
	// as zero so fraction columns degrade to zero-width instead.
	if available >= engine.UnboundedThreshold {
		available = 0
	}

	widths := make([]float64, len(g.columns))

	fixedSum, fracSum := 0.0, 0.0

	for _, c := range g.columns {
		if c.kind == widthFixed {
			fixedSum += c.value
		} else {
			fracSum += c.value
		}
	}

	totalGap := g.columnGap * math.Max(0, float64(len(g.columns)-1))

	remaining := math.Max(0, available-fixedSum-totalGap)

	for i, c := range g.columns {
		switch {
		case c.kind == widthFixed:
			widths[i] = c.value
		case fracSum > 0:
			widths[i] = remaining * (c.value / fracSum)
		}
	}

	return widths
}

func (g *GridWidget) Measure(ctx *engine.Context, c engine.Constraints) engine.Placeable {
	widths := g.resolveColumnWidths(c.MaxW)

	rows := make([][]engine.Placeable, len(g.rows))
	rowHeights := make([]float64, len(g.rows))

	for ri, row := range g.rows {
		placements := make([]engine.Placeable, len(widths))
		rowH := 0.0

		for ci := range widths {
			if ci >= len(row) || row[ci] == nil {
				continue
			}

			cc := engine.Constraints{MinW: widths[ci], MaxW: widths[ci], MinH: 0, MaxH: engine.Unbounded}
			p := row[ci].Measure(ctx, cc)
			placements[ci] = p

			if h := p.Size().H; h > rowH {
				rowH = h
			}
		}

		rows[ri] = placements
		rowHeights[ri] = rowH
	}

	totalW := g.columnGap * math.Max(0, float64(len(widths)-1))
	for _, w := range widths {
		totalW += w
	}

	totalH := g.rowGap * math.Max(0, float64(len(rowHeights)-1))
	for _, h := range rowHeights {
		totalH += h
	}

	size := c.Constrain(engine.Size{W: totalW, H: totalH})

	return &gridPlaceable{
		widths:     widths,
		rowHeights: rowHeights,
		rows:       rows,
		size:       size,
		headerRows: g.headerRows,
		columnGap:  g.columnGap,
		rowGap:     g.rowGap,
	}
}

type gridPlaceable struct {
	widths     []float64
	rowHeights []float64
	headerRows int
	rows       [][]engine.Placeable
	size       engine.Size
	columnGap  float64
	rowGap     float64
}

func (gp *gridPlaceable) Size() engine.Size { return gp.size }

func (gp *gridPlaceable) placeRow(ctx *engine.Context, row []engine.Placeable, x, cy float64) {
	cx := x

	for ci, p := range row {
		if p != nil {
			p.Place(ctx, cx, cy)
		}

		cx += gp.widths[ci] + gp.columnGap
	}
}

func (gp *gridPlaceable) Place(ctx *engine.Context, x, y float64) {
	gp.PlaceFlow(ctx, x, y)
}

// PlaceFlow implements engine.FlowPlaceable — see flexPlaceable.PlaceFlow.
// It returns the y coordinate just after the last row, on whatever page the
// table ended on.
func (gp *gridPlaceable) PlaceFlow(ctx *engine.Context, x, y float64) float64 {
	cy := y
	end := y

	for ri, row := range gp.rows {
		rh := gp.rowHeights[ri]

		// Same page-break-avoid behavior as Column, applied per row — this
		// is what lets a long line-item table split cleanly across pages
		// instead of cutting a row in half. Never break in front of a
		// header row itself (it's re-placed explicitly below).
		if ri >= gp.headerRows && cy+rh > ctx.PageBottom() && cy > ctx.PageTop() {
			ctx.NewPage()
			cy = ctx.PageTop()

			// Re-place the repeating header rows at the top of the new
			// page before continuing with the remaining body rows.
			hy := cy
			for hi := 0; hi < gp.headerRows && hi < len(gp.rows); hi++ {
				gp.placeRow(ctx, gp.rows[hi], x, hy)
				hy += gp.rowHeights[hi] + gp.rowGap
			}

			cy = hy
		}

		gp.placeRow(ctx, row, x, cy)

		end = cy + rh
		cy = end + gp.rowGap
	}

	return end
}
