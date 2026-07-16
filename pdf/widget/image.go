package widget

import engine "github.com/wssto2/go-core/pdf"

// ImageWidget draws a raw image into a fixed-size box. Sizing is always
// explicit (via Width/Height modifiers, or the constraints handed down by a
// parent like Grid) since gofpdf doesn't give us intrinsic image dimensions
// without decoding — callers that want aspect-ratio-correct sizing should
// pick Width/Height themselves based on the source asset.
type ImageWidget struct {
	data []byte
}

// Image builds an image widget from raw PNG, JPEG, or GIF bytes — the
// format is sniffed from the data's magic bytes. Unsupported formats
// surface as an error from Render/PDF (not a panic).
func Image(data []byte) *ImageWidget {
	return &ImageWidget{data: data}
}

// Modifier wraps this widget with the given (order-matters) modifiers.
func (i *ImageWidget) Modifier(mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(i, mods)
}

func (i *ImageWidget) Measure(_ *engine.Context, c engine.Constraints) engine.Placeable {
	// With no explicit size, an image collapses to nothing rather than
	// filling unbounded space — callers must size it via modifiers.
	w, h := c.MaxW, c.MaxH
	if w >= engine.UnboundedThreshold {
		w = 0
	}

	if h >= engine.UnboundedThreshold {
		h = 0
	}

	// ConstrainMax: an image's size is intrinsic/explicit (via Width/Height
	// modifiers) and must not be stretched by an ambient tight Min (e.g. a
	// Grid cell's tight column width) when no Width/Height was given.
	return &imagePlaceable{
		data: i.data,
		size: c.ConstrainMax(engine.Size{W: w, H: h}),
	}
}

type imagePlaceable struct {
	data []byte
	size engine.Size
}

func (p *imagePlaceable) Size() engine.Size { return p.size }

func (p *imagePlaceable) Place(ctx *engine.Context, x, y float64) {
	if p.size.W <= 0 || p.size.H <= 0 {
		return
	}

	ctx.Renderer.DrawImage(p.data, x, y, p.size.W, p.size.H)
}
