package pdf

// Widget is a node in the document tree. It follows Flutter/Compose's
// two-phase protocol: Measure receives Constraints from its parent and
// returns a Placeable describing the size it wants; Place is called
// afterwards — possibly by a different position/page than originally
// measured for, e.g. inside a Row that got pushed onto a new page — to
// actually draw it.
type Widget interface {
	Measure(ctx *Context, c Constraints) Placeable
}

// Placeable is the result of measuring a Widget: a known Size, plus the
// ability to draw itself at an (x, y) position handed down by its parent.
//
// A Placeable must be stateless with respect to placement: Place may be
// called more than once (e.g. GridWidget re-places its repeating header
// rows at the top of every page a table breaks onto), so drawing must not
// depend on or mutate state from a previous Place call.
type Placeable interface {
	Size() Size
	Place(ctx *Context, x, y float64)
}

// FlowPlaceable is an optional extension implemented by placeables that
// participate in vertical page flow (Column, Grid, KeepTogether): PlaceFlow
// draws the content starting at (x, y) — breaking across pages internally
// as needed — and returns the y coordinate immediately after the content on
// whatever page it ended on, so a parent Column can continue laying out
// siblings right after it instead of assuming everything landed on the page
// it started on.
//
// Place (from Placeable) must behave identically apart from not reporting
// the ending position.
type FlowPlaceable interface {
	Placeable
	PlaceFlow(ctx *Context, x, y float64) float64
}
