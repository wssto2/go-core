// Package depgraph provides a simple directed graph implementation for validating
// dependency graphs for cycles and missing nodes.
package depgraph

// Graph is a simple directed graph where nodes are identified by string names.
// It is sufficient for validating dependency graphs for cycles and missing nodes.
type Graph struct {
	nodes map[string]struct{}
	edges map[string][]string // from -> tos
}

// New creates an empty Graph.
func New() *Graph {
	return &Graph{
		nodes: make(map[string]struct{}),
		edges: make(map[string][]string),
	}
}

// AddNode ensures a node exists in the graph.
func (g *Graph) AddNode(name string) {
	if g.nodes == nil {
		g.nodes = make(map[string]struct{})
	}

	g.nodes[name] = struct{}{}
}

// AddEdge adds a directed edge from->to. Nodes are created if missing.
func (g *Graph) AddEdge(from, to string) { //nolint:varnamelen
	if g.nodes == nil {
		g.nodes = make(map[string]struct{})
	}

	if g.edges == nil {
		g.edges = make(map[string][]string)
	}

	g.nodes[from] = struct{}{}
	g.nodes[to] = struct{}{}
	g.edges[from] = append(g.edges[from], to)
}

// FindCycles returns a list of cycles found in the graph. Each cycle is
// represented as a slice of node names where the last element repeats the
// cycle start (e.g. A,B,C,A).
func (g *Graph) FindCycles() [][]string {
	var res [][]string

	color := make(map[string]int) // 0=white,1=gray,2=black

	var stack []string

	var dfs func(string)
	dfs = func(u string) {
		if color[u] == 2 { //nolint:mnd
			return
		}

		color[u] = 1

		stack = append(stack, u)
		for _, v := range g.edges[u] {
			if color[v] == 0 {
				dfs(v)
			} else if color[v] == 1 {
				// found cycle: collect from v..end of stack
				idx := -1

				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == v {
						idx = i

						break
					}
				}

				if idx >= 0 {
					cycle := make([]string, 0, len(stack)-idx+1)
					cycle = append(cycle, stack[idx:]...)
					cycle = append(cycle, v)
					res = append(res, cycle)
				}
			}
		}

		stack = stack[:len(stack)-1]
		color[u] = 2
	}

	// ensure we visit all nodes
	for n := range g.nodes {
		if color[n] == 0 {
			dfs(n)
		}
	}

	return res
}

// MissingNodes returns a list of referenced nodes that are not present in the
// provided registered list. Order is not guaranteed.
func (g *Graph) MissingNodes(registered []string) []string {
	reg := make(map[string]struct{}, len(registered))
	for _, r := range registered {
		reg[r] = struct{}{}
	}

	missingSet := make(map[string]struct{})

	for _, tos := range g.edges {
		for _, to := range tos {
			if _, ok := reg[to]; !ok {
				missingSet[to] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(missingSet))
	for m := range missingSet {
		out = append(out, m)
	}

	return out
}

// Validate returns missing nodes (referenced but not registered) and any
// cycles found in the graph.
func (g *Graph) Validate(registered []string) ([]string, [][]string) {
	cycles := g.FindCycles()
	missing := g.MissingNodes(registered)

	return missing, cycles
}
