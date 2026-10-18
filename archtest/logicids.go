package archtest

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	errNoLogicIDs = errors.New("archtest: no Logic IDs parsed from the mapping (has its format changed?)")

	// A mapping line: "- IAM-USER-001 — what it says — where".
	mappingLine = regexp.MustCompile(`(?m)^- ([A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+-\d{3}) —`)
	// A rule heading: "## IAM-USER-001 — title".
	ruleHeading = regexp.MustCompile(`(?m)^## ([A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+-\d{3}) —`)
	logicID     = regexp.MustCompile(`[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+-\d{3}`)
)

// LogicIDConfig describes a registry of Logic IDs (rules such as IAM-USER-001)
// and where the code names them.
type LogicIDConfig struct {
	// Root is the module root; empty finds it from the working directory upward.
	Root string

	// Mapping is the registry file, relative to Root. Every line that starts
	// "- ID —" declares an ID.
	Mapping string

	// Go lists the directories whose Go files may anchor an ID in a comment
	// (a string literal does not count). Text lists the directories whose
	// files, with one of the extensions TextExt, anchor it anywhere in their
	// text. A directory is walked like Sources: no testdata, vendor,
	// node_modules, hidden or underscore folders, nested modules.
	Go   []string
	Text []string
	// TextExt are the extensions of the Text files. Default .ts, .vue, .js.
	TextExt []string

	// RuleFiles are globs, relative to Root, of the files that state the rules:
	// every "## ID —" heading in them must be declared in Mapping. Empty is off;
	// Mapping itself is skipped when a glob matches it.
	RuleFiles []string

	// NotYetPorted are declared IDs with no code to anchor yet. A ratchet: one
	// that is anchored is stale and fails, so the list only shrinks.
	NotYetPorted []string
}

// LogicIDs holds the code to its registry of Logic IDs (see LogicIDConfig):
// every ID the mapping declares is named in a code comment, or in a frontend
// file, so a rule can be traced from the document to the code and back.
// Without it the anchors drift to zero unnoticed.
//
//	archtest.LogicIDs(t, archtest.LogicIDConfig{
//		Mapping:      "documentation/mapping/backend.md",
//		Go:           []string{"internal"},
//		Text:         []string{"frontend/features"},
//		NotYetPorted: []string{"VEH-AUCTION-001"},
//	})
func LogicIDs(t *testing.T, cfg LogicIDConfig) {
	t.Helper()

	problems, err := CheckLogicIDs(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, problem := range problems {
		t.Error(problem)
	}
}

// CheckLogicIDs is LogicIDs returning the problems instead of failing a test.
func CheckLogicIDs(cfg LogicIDConfig) ([]string, error) {
	root, err := resolveRoot(cfg.Root)
	if err != nil {
		return nil, err
	}

	declared, err := declaredLogicIDs(root, cfg.Mapping)
	if err != nil {
		return nil, err
	}

	anchored, err := anchoredLogicIDs(root, cfg)
	if err != nil {
		return nil, err
	}

	out := ledger(cfg, declared, anchored)

	unlisted, err := unlistedRules(root, cfg, declared)

	return append(out, unlisted...), err
}

// ledger settles the declared IDs against the anchors and the NotYetPorted ratchet.
func ledger(cfg LogicIDConfig, declared []string, anchored map[string]bool) []string {
	var missing, stale []string

	for _, ident := range declared {
		notYet := slices.Contains(cfg.NotYetPorted, ident)

		switch {
		case anchored[ident] && notYet:
			stale = append(stale, ident)
		case !anchored[ident] && !notYet:
			missing = append(missing, ident)
		}
	}

	slices.Sort(missing)
	slices.Sort(stale)

	var out []string

	if len(missing) > 0 {
		out = append(out, "UNANCHORED LOGIC IDs:\n  "+strings.Join(missing, "\n  ")+"\n\n"+
			"  -> "+cfg.Mapping+" declares these, but no comment under "+strings.Join(cfg.Go, ", ")+
			" names them, so the rule cannot be traced from the document to the code.\n"+
			"  Fix: name the ID in the doc comment of the function that implements it, e.g.\n"+
			"     \"// Create implements <ID>: a vehicle needs a VIN\". If the rule is written but not implemented, add it\n"+
			"     to NotYetPorted with a reason.")
	}

	if len(stale) > 0 {
		out = append(out, "STALE NotYetPorted:\n  "+strings.Join(stale, "\n  ")+"\n\n"+
			"  -> these now have a code anchor, so they are implemented.\n"+
			"  Fix: remove them from NotYetPorted.")
	}

	return out
}

func declaredLogicIDs(root, mapping string) ([]string, error) {
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(mapping))) //nolint:gosec // the module under test
	if err != nil {
		return nil, fmt.Errorf("archtest: the Logic ID registry: %w", err)
	}

	var declared []string

	for _, match := range mappingLine.FindAllStringSubmatch(string(src), -1) {
		declared = append(declared, match[1])
	}

	if len(declared) == 0 {
		return nil, fmt.Errorf("%w: %s", errNoLogicIDs, mapping)
	}

	return declared, nil
}

// unlistedRules: the rule headings of cfg.RuleFiles that the mapping does not declare.
func unlistedRules(root string, cfg LogicIDConfig, declared []string) ([]string, error) {
	var out []string

	for _, pattern := range cfg.RuleFiles {
		files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, fmt.Errorf("archtest: RuleFiles %q: %w", pattern, err)
		}

		for _, file := range files {
			rel, _ := filepath.Rel(root, file)
			rel = filepath.ToSlash(rel)

			if rel == filepath.ToSlash(cfg.Mapping) {
				continue
			}

			src, err := os.ReadFile(file) //nolint:gosec // a rule file of the module under test
			if err != nil {
				return nil, fmt.Errorf("archtest: reading %s: %w", rel, err)
			}

			for _, match := range ruleHeading.FindAllStringSubmatch(string(src), -1) {
				if !slices.Contains(declared, match[1]) {
					out = append(out, rel+" states "+match[1]+", which "+cfg.Mapping+" does not list: add a line for it")
				}
			}
		}
	}

	return out, nil
}

// anchoredLogicIDs collects the IDs named in the Go comments of cfg.Go and in
// the text of cfg.Text. Go files are parsed with their comments (Sources
// leaves them out); a file that does not parse is an error, not a silent gap.
func anchoredLogicIDs(root string, cfg LogicIDConfig) (map[string]bool, error) {
	found := map[string]bool{}
	fset := token.NewFileSet()

	collect := func(text string) {
		for _, ident := range logicID.FindAllString(text, -1) {
			found[ident] = true
		}
	}

	exts := cfg.TextExt
	if len(exts) == 0 {
		exts = []string{".ts", ".vue", ".js"}
	}

	for _, dir := range cfg.Go {
		err := walkFiles(root, dir, func(full string) error { return goComments(root, fset, full, collect) })
		if err != nil {
			return nil, err
		}
	}

	for _, dir := range cfg.Text {
		err := walkFiles(root, dir, func(full string) error { return fileText(root, full, exts, collect) })
		if err != nil {
			return nil, err
		}
	}

	return found, nil
}

func goComments(root string, fset *token.FileSet, full string, collect func(string)) error {
	if !strings.HasSuffix(full, ".go") {
		return nil
	}

	parsed, err := parser.ParseFile(fset, full, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", relTo(root, full), err)
	}

	for _, group := range parsed.Comments {
		collect(group.Text())
	}

	return nil
}

func fileText(root, full string, exts []string, collect func(string)) error {
	if !slices.Contains(exts, filepath.Ext(full)) {
		return nil
	}

	src, err := os.ReadFile(full) //nolint:gosec // a source file of the module under test
	if err != nil {
		return fmt.Errorf("reading %s: %w", relTo(root, full), err)
	}

	collect(string(src))

	return nil
}

func relTo(root, full string) string {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return full
	}

	return filepath.ToSlash(rel)
}

// walkFiles calls visit for every file under root/dir that the module walk would not skip.
func walkFiles(root, dir string, visit func(full string) error) error {
	start := filepath.Join(root, filepath.FromSlash(dir))

	err := filepath.WalkDir(start, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if full == start {
				return nil
			}

			return visitDir(root, full, entry.Name())
		}

		return visit(full)
	})
	if err != nil {
		return fmt.Errorf("archtest: reading %s: %w", dir, err)
	}

	return nil
}
