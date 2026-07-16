package widget

import engine "github.com/wssto2/go-core/pdf"

// Box is sugar for engine.ApplyModifiers(child, mods...), useful for
// readability parity with Text's option pattern and for wrapping a widget
// inline without needing its own concrete type's .Modifier() method.
func Box(child engine.Widget, mods ...engine.Modifier) engine.Widget {
	return engine.ApplyModifiers(child, mods)
}
