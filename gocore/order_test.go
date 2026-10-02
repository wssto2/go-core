package gocore_test

import (
	"os/exec"
	"strings"
	"testing"
)

// A dependency used before the line that creates it does not compile: that is
// what makes Install order a matter for the compiler instead of a runtime check.
func TestInstallOrderIsCheckedByTheCompiler(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two programs")
	}

	build := func(dir string) (string, error) {
		out, err := exec.CommandContext(t.Context(), "go", "build", "-o", "/dev/null", "./testdata/"+dir).CombinedOutput() //nolint:gosec // fixed command, dir is a literal from this test
		return string(out), err
	}

	if out, err := build("inorder"); err != nil {
		t.Fatalf("installing in data-flow order must build: %v\n%s", err, out)
	}

	out, err := build("usedbeforeexists")
	if err == nil {
		t.Fatal("installing a feature before its dependency exists must not compile")
	}

	if !strings.Contains(out, "undefined: users") {
		t.Fatalf("want error \"undefined: users\", got:\n%s", out)
	}
}
