package dbtest_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wssto2/go-core/database/dbtest"
)

// The check catches each construct, and leaves alone what MariaDB 10.3 accepts.
func TestPortabilityCatchesEachConstruct(t *testing.T) {
	bad := map[string]string{
		"SELECT id FROM t FOR UPDATE SKIP LOCKED;":                "SKIP LOCKED",
		"SELECT id FROM t FOR UPDATE OF t;":                       "FOR UPDATE OF",
		"SELECT id FROM t FOR SHARE;":                             "FOR SHARE",
		"SELECT j.id FROM JSON_TABLE(d, '$' COLUMNS (id INT)) j;": "JSON_TABLE",
		"SELECT doc->>'$.id' FROM t;":                             "->>",
		"INSERT INTO t (a) VALUES (1) RETURNING id;":              "RETURNING",
	}
	good := []string{
		"SELECT id FROM t WHERE id = ? FOR UPDATE;",
		"SELECT id FROM t LOCK IN SHARE MODE;",
		"-- FOR SHARE is only mentioned in a comment\nSELECT 1;",
		"CREATE TABLE returning_customers (id INT);",
		"SELECT ROW_NUMBER() OVER (PARTITION BY a ORDER BY b) FROM t;",
	}

	for sql, want := range bad {
		found, err := dbtest.Portability(fstest.MapFS{"a.sql": {Data: []byte(sql)}})
		if err != nil || len(found) != 1 || !strings.Contains(found[0].What, want) {
			t.Errorf("%q: got %v, %v; want %s", sql, found, err, want)
		}
	}

	for _, sql := range good {
		found, err := dbtest.Portability(fstest.MapFS{"a.sql": {Data: []byte(sql)}})
		if err != nil || len(found) != 0 {
			t.Errorf("%q wrongly reported: %v, %v", sql, found, err)
		}
	}
}

func TestPortabilityNamesFileAndLine(t *testing.T) {
	found, _ := dbtest.Portability(fstest.MapFS{"x/b.sql": {Data: []byte("SELECT 1;\nSELECT 2 FOR SHARE;")}})
	if len(found) != 1 || found[0].String() != "x/b.sql:2: FOR SHARE (MySQL 8; use LOCK IN SHARE MODE or FOR UPDATE)" {
		t.Fatalf("got %v", found)
	}
}
