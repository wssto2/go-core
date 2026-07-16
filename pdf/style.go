package pdf

// Edges describes a four-sided spacing value (padding/margin), e.g. for
// use with the Padding modifier.
type Edges struct {
	Top, Right, Bottom, Left float64
}

// All returns Edges with the same value on all four sides.
func All(v float64) Edges {
	return Edges{v, v, v, v}
}
