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
	}
	for _, phase := range phases {
		// Require that the phase marker exists in PLAN.md (either [ ] in-progress or [x] done).
		// Fail only if the marker is missing entirely — not if a phase is still in progress.
		inProgress := "* [ ] " + phase
		done := "* [x] " + phase
		if !strings.Contains(content, inProgress) && !strings.Contains(content, done) {
			t.Errorf("PLAN.md is missing phase marker for %s (expected %q or %q)", phase, inProgress, done)
		}
	}
}
