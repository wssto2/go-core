package depgraph

import "testing"

func containsSequence(slice []string, a, b, c string) bool {
	for i := 0; i+2 < len(slice); i++ {
		if slice[i] == a && slice[i+1] == b && slice[i+2] == c {
			return true
		}
	}
	return false
}

func TestFindCycles(t *testing.T) {
	g := New()
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")
	g.AddEdge("C", "A")
	cycles := g.FindCycles()
	if len(cycles) == 0 {
		t.Fatalf("expected cycles, got none")
	}
	found := false
	for _, cyc := range cycles {
		if containsSequence(cyc, "A", "B", "C") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cycle containing A,B,C, got: %#v", cycles)
	}
}

func TestMissingNodes(t *testing.T) {
	g := New()
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")
	missing := g.MissingNodes([]string{"A", "B"})
	if len(missing) != 1 {
		t.Fatalf("expected 1 missing node, got %d: %v", len(missing), missing)
	}
	if missing[0] != "C" {
		t.Fatalf("expected missing C, got %v", missing)
	}
}

func TestValidateNoIssues(t *testing.T) {
	g := New()
	g.AddEdge("A", "B")
	g.AddEdge("B", "C")
	missing, cycles := g.Validate([]string{"A", "B", "C"})
	if len(missing) != 0 {
		t.Fatalf("expected no missing, got %v", missing)
	}
	if len(cycles) != 0 {
		t.Fatalf("expected no cycles, got %v", cycles)
	}
}
