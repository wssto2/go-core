package order

// No import of the framework: .Modules here is some other method.
func other(x interface{ Modules() }) { x.Modules() }
