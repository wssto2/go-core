package dbtest

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/wssto2/go-core/archtest"
)

// Violation is one piece of SQL that MariaDB 10.3 rejects, or that the house
// rules forbid so a query cannot depend on it.
type Violation struct {
	File string
	Line int
	What string
}

func (v Violation) String() string { return v.File + ":" + strconv.Itoa(v.Line) + ": " + v.What }

// Portability scans every .sql file of fsys, comments left out, and returns what
// MariaDB 10.3 rejects: the constructs of archtest.SQLRules (SKIP LOCKED, FOR
// UPDATE OF, FOR SHARE, JSON_TABLE, ->>, RETURNING). The same table holds the Go
// sources to it through archtest's SQLPortability rule. Run it over every
// migration FS a module ships:
//
//	dbtest.RequirePortable(t, migrations)
func Portability(fsys fs.FS) ([]Violation, error) {
	out, _, err := scanFS(fsys)

	return out, err
}

// scanFS is Portability and the number of .sql files it read.
func scanFS(fsys fs.FS) ([]Violation, int, error) {
	var (
		out   []Violation
		files int
	)

	rules := archtest.SQLRules()

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return err
		}

		src, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}

		files++

		out = append(out, scan(path, string(src), rules)...)

		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("dbtest: scanning SQL: %w", err)
	}

	return out, files, nil
}

func scan(file, src string, rules []archtest.SQLRule) []Violation {
	var out []Violation

	for i, line := range strings.Split(src, "\n") {
		code, _, _ := strings.Cut(line, "--")
		if code == "" {
			continue
		}

		for _, rule := range rules {
			if rule.Pattern.MatchString(code) {
				out = append(out, Violation{File: file, Line: i + 1, What: rule.What})
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

	found, files, err := scanFS(fsys)
	if err != nil {
		tb.Fatal(err)
	}

	for _, v := range found {
		tb.Errorf("SQL that MariaDB 10.3.39 rejects: %s", v)
	}

	if files == 0 {
		tb.Errorf("dbtest: no .sql files in the FS, nothing was checked")
	}
}
