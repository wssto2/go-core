package scaffold_test

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/wssto2/go-core/cmd/go-core/scaffold"
)

const goldenDir = "testdata/feature"

func sample(t *testing.T) scaffold.FeatureData {
	t.Helper()

	data, err := scaffold.NewFeatureData("example.com/shop", "crm", "note", "")
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func TestNewFeatureDataValidatesTheNames(t *testing.T) {
	t.Parallel()

	cases := map[string][3]string{
		"domain with a dash":    {"my-crm", "note", ""},
		"domain with a capital": {"Crm", "note", ""},
		"empty feature":         {"crm", " ", ""},
		"feature with a dot":    {"crm", "no.te", ""},
		"feature is a keyword":  {"crm", "type", ""},
		"domain is a keyword":   {"func", "note", ""},
		"entity lowercase":      {"crm", "note", "note"},
		"entity with a dash":    {"crm", "note", "Note-Item"},
	}

	for name, in := range cases {
		if _, err := scaffold.NewFeatureData("example.com/shop", in[0], in[1], in[2]); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	data, err := scaffold.NewFeatureData("example.com/shop", "crm", "note", "")
	if err != nil || data.Entity != "Note" || data.Namespace != "crm.note" || data.InputType != "CreateNoteInput" {
		t.Errorf("defaults: %+v, %v", data, err)
	}

	data, err = scaffold.NewFeatureData("example.com/shop", "crm", "note", "Remark")
	if err != nil || data.Entity != "Remark" || data.RepoType != "RemarkRepository" {
		t.Errorf("entity: %+v, %v", data, err)
	}
}

func TestGenerateFeatureMatchesTheGoldenTree(t *testing.T) {
	root := t.TempDir()

	written, err := scaffold.GenerateFeature(root, sample(t))
	if err != nil {
		t.Fatal(err)
	}

	if len(written) != 11 {
		t.Fatalf("want 11 files, got %d: %v", len(written), written)
	}

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.RemoveAll(goldenDir); err != nil {
			t.Fatal(err)
		}

		for _, rel := range written {
			copyFile(t, filepath.Join(root, rel), filepath.Join(goldenDir, rel+".golden"))
		}
	}

	var golden []string

	err = filepath.WalkDir(goldenDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && !entry.IsDir() {
			rel, _ := filepath.Rel(goldenDir, path)
			golden = append(golden, strings.TrimSuffix(rel, ".golden"))
		}

		return walkErr
	})
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(golden)

	got := slices.Clone(written)
	slices.Sort(got)

	if !slices.Equal(got, golden) {
		t.Fatalf("generated files differ from the golden tree\n got: %v\nwant: %v", got, golden)
	}

	for _, rel := range written {
		want, err := os.ReadFile(filepath.Join(goldenDir, rel+".golden"))
		if err != nil {
			t.Fatal(err)
		}

		have, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(have, want) {
			t.Errorf("%s differs from its golden file (UPDATE_GOLDEN=1 go test ./scaffold rewrites them)\n--- got\n%s", rel, have)
		}
	}
}

func TestGenerateFeatureRefusesToOverwrite(t *testing.T) {
	root := t.TempDir()

	clash := filepath.Join(root, "internal", "application", "crm", "note", "service.go")
	if err := os.MkdirAll(filepath.Dir(clash), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(clash, []byte("package mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := scaffold.GenerateFeature(root, sample(t))
	if err == nil || !strings.Contains(err.Error(), "internal/application/crm/note/service.go") {
		t.Fatalf("want a refusal naming the file, got %v", err)
	}

	if got, _ := os.ReadFile(clash); string(got) != "package mine\n" {
		t.Errorf("the existing file was changed: %s", got)
	}

	if _, statErr := os.Stat(filepath.Join(root, "internal", "domain")); statErr == nil {
		t.Error("a refusal must write nothing")
	}
}

func TestFindModuleReadsGoMod(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("// c\nmodule example.com/shop // x\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deep := filepath.Join(root, "internal", "a")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	gotRoot, module, err := scaffold.FindModule(deep)
	if err != nil || gotRoot != root || module != "example.com/shop" {
		t.Errorf("got %q %q %v", gotRoot, module, err)
	}

	if _, _, err := scaffold.FindModule(t.TempDir()); err == nil {
		t.Error("a directory outside any module is an error")
	}
}

func TestFeatureStepsKeepOnlyWhatIsManual(t *testing.T) {
	t.Parallel()

	steps := scaffold.FeatureSteps(sample(t))

	for _, want := range []string{`"example.com/shop/internal/bootstrap/note"`, "note.Install(app)", "crm.note:view", "navigation"} {
		if !strings.Contains(steps, want) {
			t.Errorf("steps lack %q:\n%s", want, steps)
		}
	}

	for _, gone := range []string{"tsContracts", "Contract", "generate"} {
		if strings.Contains(steps, gone) {
			t.Errorf("steps still mention %q:\n%s", gone, steps)
		}
	}
}

// TestGeneratedTreeCompiles builds and vets the tree in a temporary module that uses this checkout of go-core.
func TestGeneratedTreeCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a module")
	}

	core, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	coreMod, err := os.ReadFile(filepath.Join(core, "go.mod"))
	if err != nil || !bytes.HasPrefix(coreMod, []byte("module github.com/wssto2/go-core\n")) {
		t.Skip("this module is not inside a go-core checkout")
	}

	goVersion := regexp.MustCompile(`(?m)^go (\S+)$`).FindSubmatch(coreMod)
	if goVersion == nil {
		t.Fatal("go-core's go.mod has no go directive")
	}

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}

	app := t.TempDir()
	mod := "module example.com/shop\n\ngo " + string(goVersion[1]) + "\n\nrequire github.com/wssto2/go-core v0.0.0\n\nreplace github.com/wssto2/go-core => " + core + "\n"

	if err := os.WriteFile(filepath.Join(app, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}

	copyFile(t, filepath.Join(core, "go.sum"), filepath.Join(app, "go.sum"))

	if _, err := scaffold.GenerateFeature(app, sample(t)); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.CommandContext(t.Context(), "go", args...)
		cmd.Dir = app
		// -mod=mod completes go.mod from go-core's requirements, offline from the module cache.
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()

	raw, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(to, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
