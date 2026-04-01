package dicheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRuntimeDIUsage(t *testing.T) {
	tmp, err := os.MkdirTemp("", "dicheck_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	// Create allowed dir bootstrap
	bootstrapDir := filepath.Join(tmp, "bootstrap")
	if err := os.MkdirAll(bootstrapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bootstrapFile := filepath.Join(bootstrapDir, "container.go")
	if err := os.WriteFile(bootstrapFile, []byte("package bootstrap\nfunc Bind() {}\nfunc Resolve() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create other dir with runtime DI usage
	modDir := filepath.Join(tmp, "module")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	modFile := filepath.Join(modDir, "use.go")
	if err := os.WriteFile(modFile, []byte("package mod\nfunc foo() { Resolve[My](c) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bindFile := filepath.Join(modDir, "bind.go")
	if err := os.WriteFile(bindFile, []byte("package mod\nfunc bar() { Bind(c, v) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	matches, err := FindRuntimeDIUsage(tmp, []string{"bootstrap"})
	if err != nil {
		t.Fatalf("FindRuntimeDIUsage error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %#v", len(matches), matches)
	}
	found := map[string]bool{}
	for _, m := range matches {
		found[m] = true
	}
	if !found[modFile] {
		t.Fatalf("expected match %s", modFile)
	}
	if !found[bindFile] {
		t.Fatalf("expected match %s", bindFile)
	}
	if found[bootstrapFile] {
		t.Fatalf("did not expect match %s", bootstrapFile)
	}
}
