package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runNewFeature(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := newFeatureCmd()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return out.String(), err
}

func TestNewFeatureWritesTheTreeInTheModuleOfTheWorkingDirectory(t *testing.T) {
	root := t.TempDir()

	err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shop\n\ngo 1.26\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(root, "cmd", "api")

	err = os.MkdirAll(sub, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	t.Chdir(sub)

	out, err := runNewFeature(t, "--domain", "crm", "--feature", "note", "--entity", "Remark")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"crm/note", "internal/bootstrap/note/install.go", "note.Install(app)", "RemarkRepository"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}

	raw, err := os.ReadFile(filepath.Join(root, "internal", "domain", "crm", "note", "note.go"))
	if err != nil || !strings.Contains(string(raw), "type Remark struct") {
		t.Errorf("the entity is written in the module root: %v\n%s", err, raw)
	}

	_, err = runNewFeature(t, "--domain", "crm", "--feature", "note", "--entity", "Remark")
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("a second run must refuse, got %v", err)
	}
}

func TestNewFeatureRejectsBadInput(t *testing.T) {
	root := t.TempDir()

	err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/shop\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)

	for name, args := range map[string][]string{
		"no flags":         {},
		"domain with dash": {"--domain", "my-crm", "--feature", "note"},
		"keyword feature":  {"--domain", "crm", "--feature", "type"},
	} {
		_, err := runNewFeature(t, args...)
		if err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	if _, statErr := os.Stat(filepath.Join(root, "internal")); statErr == nil {
		t.Error("nothing is written on bad input")
	}
}
