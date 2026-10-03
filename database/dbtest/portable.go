package dbtest

import (
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Violation is one piece of SQL that MariaDB 10.3 rejects, or that the house
// rules forbid so a query cannot depend on it.
type Violation struct {
	File string
	Line int
	What string
}

func (v Violation) String() string { return v.File + ":" + strconv.Itoa(v.Line) + ": " + v.What }

// Every market runs MariaDB 10.3.39, which rejects these MySQL 8 / newer
// MariaDB constructs with Error 1064, while local MySQL accepts them, so
// nothing else would catch one. Rewrite the query instead: a single-table
// SELECT ... FOR UPDATE by primary key replaces SKIP LOCKED and FOR UPDATE OF;
// FOR UPDATE (or LOCK IN SHARE MODE) replaces FOR SHARE. Window functions are
// fine: MariaDB has them since 10.2.
var forbidden = []struct {
	re   *regexp.Regexp
	what string
}{
	{regexp.MustCompile(`(?i)\bSKIP\s+LOCKED\b`), "SKIP LOCKED (MariaDB 10.6+)"},
	{regexp.MustCompile(`(?i)\bFOR\s+UPDATE\s+OF\b`), "FOR UPDATE OF <table> (not in MariaDB)"},
	{regexp.MustCompile(`(?i)\bFOR\s+SHARE\b`), "FOR SHARE (MySQL 8; use LOCK IN SHARE MODE or FOR UPDATE)"},
	{regexp.MustCompile(`(?i)\bJSON_TABLE\s*\(`), "JSON_TABLE (MariaDB 10.6+)"},
	{regexp.MustCompile(`->>`), "->> (not in MariaDB)"},
	{regexp.MustCompile(`(?i)\bRETURNING\b`), "RETURNING (MariaDB 10.5+)"},
}

// Portability scans every .sql file of fsys, comments left out, and returns what
// MariaDB 10.3 rejects: SKIP LOCKED, FOR UPDATE OF, FOR SHARE, JSON_TABLE, ->>,
// and RETURNING. Run it over every migration FS a module ships:
//
//	dbtest.RequirePortable(t, migrations)
func Portability(fsys fs.FS) ([]Violation, error) {
	var out []Violation

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return err
		}

		src, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}

		out = append(out, scan(path, string(src))...)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("dbtest: scanning SQL: %w", err)
	}

	return out, nil
}

func scan(file, src string) []Violation {
	var out []Violation

	for i, line := range strings.Split(src, "\n") {
		code, _, _ := strings.Cut(line, "--")
		if code == "" {
			continue
		}

		for _, f := range forbidden {
			if f.re.MatchString(code) {
				out = append(out, Violation{File: file, Line: i + 1, What: f.what})
			}
		}
	}

	return out
}

// RequirePortable fails the test for every construct Portability finds in
// fsys, and when fsys has no SQL at all (a guard that scans nothing passes
// forever).
func RequirePortable(tb testing.TB, fsys fs.FS) {
	tb.Helper()

	found, err := Portability(fsys)
	if err != nil {
		tb.Fatal(err)
	}

	for _, v := range found {
		tb.Errorf("SQL that MariaDB 10.3.39 rejects: %s", v)
	}

	if files, _ := fs.Glob(fsys, "*.sql"); len(files) == 0 {
		tb.Errorf("dbtest: no .sql files at the root of the FS, nothing was checked")
	}
}
