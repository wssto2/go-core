package pdf_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wssto2/go-core/pdf"
	"github.com/wssto2/go-core/pdf/widget"
)

// gofpdfFont reads a TTF that ships with the gofpdf module, so the test needs
// no font of its own.
func gofpdfFont(t *testing.T, name string) []byte {
	t.Helper()

	const module = "github.com/phpdave11/gofpdf"

	out, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Dir}}", module).Output()
	if err != nil {
		t.Skipf("locating the gofpdf module: %v", err)
	}

	root, err := os.OpenRoot(filepath.Join(strings.TrimSpace(string(out)), "font"))
	if err != nil {
		t.Skipf("opening the gofpdf fonts: %v", err)
	}

	defer func() { _ = root.Close() }()

	data, err := root.ReadFile(name)
	if err != nil {
		t.Skipf("reading %s: %v", name, err)
	}

	return data
}

// Rendering must leave the caller's font bytes untouched: gofpdf writes into
// the bytes it is given while subsetting, and callers share one go:embed'ed
// slice across every document, so concurrent renders would race on it.
func TestRenderLeavesTheFontBytesUntouched(t *testing.T) {
	t.Parallel()

	fonts := pdf.FontSet{
		Family:  "DejaVu",
		Regular: gofpdfFont(t, "DejaVuSansCondensed.ttf"),
		Bold:    gofpdfFont(t, "DejaVuSansCondensed-Bold.ttf"),
		Light:   nil,
	}
	regular, bold := bytes.Clone(fonts.Regular), bytes.Clone(fonts.Bold)

	var renders sync.WaitGroup
	for range 4 {
		renders.Go(func() {
			out, err := pdf.Page(widget.Text("Čćžšđ test", widget.Bold())).PDF(fonts)
			if err != nil || !bytes.HasPrefix(out, []byte("%PDF-")) {
				t.Errorf("render: %v", err)
			}
		})
	}

	renders.Wait()

	if !bytes.Equal(fonts.Regular, regular) || !bytes.Equal(fonts.Bold, bold) {
		t.Fatal("rendering wrote into the caller's font bytes")
	}
}
