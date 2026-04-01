package testhelpers

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPlanPhasesMarked(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("could not determine test file path")
	}
	planPath := filepath.Join(filepath.Dir(filename), "..", "PLAN.md")
	b, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("unable to read PLAN.md: %v", err)
	}
	content := string(b)

	phases := []string{
		"Phase 1",
		"Phase 2",
		"Phase 3",
		"Phase 4",
		"Phase 5",
		"Phase 6",
		"Phase 7",
		"Phase 8",
	}
	for _, phase := range phases {
		marker := "* [x] " + phase
		if !strings.Contains(content, marker) {
			t.Errorf("PLAN.md does not mark %s as done (missing: %q)", phase, marker)
		}
	}
}
