package go2ts

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// The modes of the generated folder and its files: sources the whole team reads.
const (
	dirMode  = 0o755
	fileMode = 0o644
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
	err := os.MkdirAll(dir, dirMode)
	if err != nil {
		return fmt.Errorf("go2ts: create %s: %w", dir, err)
	}

	out, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("go2ts: open %s: %w", dir, err)
	}

	defer func() { _ = out.Close() }() // best-effort: nothing is written through it after Sync returns

	scratch, err := os.MkdirTemp("", "go2ts-")
	if err != nil {
		return fmt.Errorf("go2ts: create the scratch folder: %w", err)
	}

	defer func() { _ = os.RemoveAll(scratch) }() // best-effort: a scratch folder of the temp dir

	err = gen(scratch)
	if err != nil {
		return err
	}

	written, err := copyChanged(scratch, out)
	if err != nil {
		return err
	}

	return removeStale(dir, out, written)
}

// copyChanged writes the files of the scratch folder that differ from the ones in out, and returns the names of all of
// them.
func copyChanged(scratch string, out *os.Root) (map[string]bool, error) {
	entries, err := os.ReadDir(scratch)
	if err != nil {
		return nil, fmt.Errorf("go2ts: read the scratch folder: %w", err)
	}

	written := make(map[string]bool, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		written[name] = true

		data, err := os.ReadFile(filepath.Join(scratch, name)) //nolint:gosec // our own scratch directory
		if err != nil {
			return nil, fmt.Errorf("go2ts: read generated %s: %w", name, err)
		}

		old, err := out.ReadFile(name)
		if err == nil && bytes.Equal(old, data) {
			continue
		}

		err = out.WriteFile(name, data, fileMode)
		if err != nil {
			return nil, fmt.Errorf("go2ts: write %s: %w", name, err)
		}
	}

	return written, nil
}

// removeStale deletes the top-level .ts files of dir that are not in written.
func removeStale(dir string, out *os.Root, written map[string]bool) error {
	existing, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("go2ts: read %s: %w", dir, err)
	}

	for _, entry := range existing {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".ts" || written[name] {
			continue
		}

		err = out.Remove(name)
		if err != nil {
			return fmt.Errorf("go2ts: remove stale %s: %w", name, err)
		}
	}

	return nil
}
