// Package docs holds the check that go-core's business rules, written in
// docs/rules, are anchored in code. It has no code of its own.
package docs_test

import (
	"testing"

	"github.com/wssto2/go-core/archtest"
)

// TestLogicIDsAreAnchoredInCode holds docs/rules/mapping.md to the code: every
// Logic ID it declares is named in a Go comment somewhere in the module, and
// every rule heading of docs/rules is listed in the mapping. Nothing is
// waiting to be implemented, and nothing should be added to NotYetPorted.
func TestLogicIDsAreAnchoredInCode(t *testing.T) {
	archtest.LogicIDs(t, archtest.LogicIDConfig{
		Root:      "..",
		Mapping:   "docs/rules/mapping.md",
		Go:        []string{"."},
		RuleFiles: []string{"docs/rules/*/*.md"},
	})
}
