package go2ts

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// Sync makes dir hold exactly what gen writes: gen runs into a scratch directory, a file is written to dir only when
// its content changed (so the timestamps of the others, and the file watchers on them, are left alone), and every
// *.ts file in dir that this run did not write is deleted. Anything that is not a top-level .ts file (subdirectories,
// other extensions) is left alone. It is how a command regenerates a folder that is checked in:
//
//	err := go2ts.Sync("frontend/generated/entities", func(dir string) error {
//		return go2ts.GenerateTypes(entities, dir)
//	})
//
// gen's error is returned as it is, and then dir is untouched.
func Sync(dir string, gen func(dir string) error) error {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:mnd // a folder of generated sources
		return fmt.Errorf("go2ts: create %s: %w", dir, err)
	}

	out, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("go2ts: open %s: %w", dir, err)
	}
	defer out.Close()

	tmp, err := os.MkdirTemp("", "go2ts-")
	if err != nil {
		return fmt.Errorf("go2ts: create the scratch folder: %w", err)
	}
	defer os.RemoveAll(tmp)

	if err := gen(tmp); err != nil {
		return err
	}

	written, err := os.ReadDir(tmp)
	if err != nil {
		return fmt.Errorf("go2ts: read the scratch folder: %w", err)
	}

	keep := make(map[string]bool, len(written))

	for _, e := range written {
		if e.IsDir() {
			continue
		}

		keep[e.Name()] = true

		data, err := os.ReadFile(filepath.Join(tmp, e.Name()))
		if err != nil {
			return fmt.Errorf("go2ts: read generated %s: %w", e.Name(), err)
		}

		if old, err := out.ReadFile(e.Name()); err == nil && bytes.Equal(old, data) {
			continue
		}

		if err := out.WriteFile(e.Name(), data, 0o644); err != nil { //nolint:gosec,mnd // generated sources are for the whole team to read
			return fmt.Errorf("go2ts: write %s: %w", e.Name(), err)
		}
	}

	existing, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("go2ts: read %s: %w", dir, err)
	}

	for _, e := range existing {
		if e.IsDir() || filepath.Ext(e.Name()) != ".ts" || keep[e.Name()] {
			continue
		}

		if err := out.Remove(e.Name()); err != nil {
			return fmt.Errorf("go2ts: remove stale %s: %w", e.Name(), err)
		}
	}

	return nil
}
