// Package migrations holds the goose migrations of the characteristics module, as MariaDB 10.3 / MySQL DDL: the tables
// characteristics_level1, _level2 and _level3, in the shape site-builder's tables have (levels with a name, its
// translations as JSON, an order, an active flag, the audit columns and a JSON meta), without its extra columns. A
// project adds its own columns in its own migration and describes them (core.Field).
//
// There are three levels, one file each. Files(n) is the first n of them, for a project that needs fewer:
// characteristics.Install(app, tree, characteristics.WithLevels(2)) registers Files(2). All files use IF NOT EXISTS,
// so a project that already has the tables adopts the files without a change. A released file is never edited.
package migrations

import (
	"embed"
	"io/fs"
)

// Levels is how many levels the module ships.
const Levels = 3

// Files are the migrations of all three levels, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS

// First returns the migrations of the first n levels (1 to Levels).
func First(n int) fs.FS { return first{n: n} }

type first struct{ n int }

func (f first) Open(name string) (fs.File, error) {
	if name != "." && !f.has(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	return Files.Open(name) //nolint:wrapcheck // the embed's own error
}

func (f first) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := Files.ReadDir(name)
	if err != nil {
		return nil, err //nolint:wrapcheck // the embed's own error
	}

	kept := entries[:0:0]

	for _, entry := range entries {
		if f.has(entry.Name()) {
			kept = append(kept, entry)
		}
	}

	return kept, nil
}

// has reports whether the file belongs to one of the first n levels: the files are numbered by level in the last
// digit of their version.
func (f first) has(name string) bool {
	const digit = len("2026110700000")

	return len(name) > digit && name[digit] >= '1' && int(name[digit]-'0') <= f.n
}
