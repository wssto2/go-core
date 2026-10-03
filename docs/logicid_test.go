// Package docs holds the check that go-core's business rules, written in
// docs/rules, are anchored in code. It has no code of its own.
package docs_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	// A mapping line: "- IAM-USER-001 — what it says — where".
	mappingLine = regexp.MustCompile(`(?m)^- ([A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+-\d{3}) —`)
	// A rule heading: "## IAM-USER-001 — title".
	ruleHeading = regexp.MustCompile(`(?m)^## ([A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+-\d{3}) —`)
	logicID     = regexp.MustCompile(`[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+-\d{3}`)
)

// notYetImplemented are Logic IDs the mapping declares but no code anchors yet.
// A ratchet: an entry that does have an anchor is stale and fails, so
// implementing a rule shrinks the list. It starts empty and should stay so.
var notYetImplemented = []string{}

func moduleRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs("..")
	require.NoError(t, err)

	return root
}

// TestLogicIDsAreAnchoredInCode holds docs/rules/mapping.md to the code: every
// Logic ID it declares is named in a Go comment somewhere in the module, so a
// rule can be traced to the code that implements it and back. It also holds
// the rule files to the mapping: a rule heading missing from it fails.
func TestLogicIDsAreAnchoredInCode(t *testing.T) {
	root := moduleRoot(t)

	mapping, err := os.ReadFile(filepath.Join(root, "docs", "rules", "mapping.md"))
	require.NoError(t, err, "docs/rules/mapping.md must exist: it is the Logic ID registry")

	var declared []string
	for _, m := range mappingLine.FindAllStringSubmatch(string(mapping), -1) {
		declared = append(declared, m[1])
	}

	require.NotEmpty(t, declared, "no Logic IDs parsed from mapping.md: has its format changed?")

	anchored := anchoredIDs(t, root)

	var missing, stale []string

	for _, id := range declared {
		switch {
		case anchored[id] && slices.Contains(notYetImplemented, id):
			stale = append(stale, id)
		case !anchored[id] && !slices.Contains(notYetImplemented, id):
			missing = append(missing, id)
		}
	}

	if len(missing) > 0 {
		t.Errorf("UNANCHORED LOGIC IDs:\n  %s\n\n"+
			"  -> docs/rules/mapping.md declares these, but no Go comment names them.\n"+
			"  Fix: name the ID in the doc comment of the function that implements it, e.g.\n"+
			"     \"// Login signs a person in (IAM-USER-001)\"; or, if the rule is written but not\n"+
			"     implemented, list it in notYetImplemented with a reason.", strings.Join(missing, "\n  "))
	}

	if len(stale) > 0 {
		t.Errorf("STALE notYetImplemented:\n  %s\n\n  -> these now have a code anchor. Fix: remove them from the list.", strings.Join(stale, "\n  "))
	}

	rules, err := filepath.Glob(filepath.Join(root, "docs", "rules", "*", "*.md"))
	require.NoError(t, err)

	for _, file := range rules {
		if filepath.Base(file) == "mapping.md" { // a module's lines for the registry, not a rule file
			continue
		}

		src, err := os.ReadFile(file) //nolint:gosec // a rule file of this repository
		require.NoError(t, err)

		for _, m := range ruleHeading.FindAllStringSubmatch(string(src), -1) {
			require.Containsf(t, declared, m[1], "%s states %s, which docs/rules/mapping.md does not list: add a line for it", file, m[1])
		}
	}
}

// anchoredIDs collects every Logic ID named in a Go comment anywhere in the
// module (a string literal does not count).
func anchoredIDs(t *testing.T, root string) map[string]bool {
	t.Helper()

	found := map[string]bool{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			switch d.Name() {
			case "testdata", "vendor", "node_modules", ".git", "docs":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil //nolint:nilerr // a file that does not parse is not this test's finding
		}

		for _, group := range parsed.Comments {
			for _, id := range logicID.FindAllString(group.Text(), -1) {
				found[id] = true
			}
		}

		return nil
	})
	require.NoError(t, err)

	return found
}
