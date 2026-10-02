package dbtest_test

import (
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/gorm"
)

// Run gives the test an empty database per target: SQLite always, MySQL and
// MariaDB when GOCORE_MYSQL_DSN and GOCORE_MARIADB_DSN are set.
func ExampleRun() {
	_ = func(t *testing.T) {
		dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
			if err := db.Exec("CREATE TABLE things (id INTEGER)").Error; err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Open is Run for one target, for a test that wants to set up several
// databases itself. It reports false when the DSN variable is unset.
func ExampleOpen() {
	_ = func(t *testing.T) {
		db, ok := dbtest.Open(t, dbtest.MariaDB)
		if !ok {
			t.Skip("GOCORE_MARIADB_DSN is not set")
		}

		_ = db
	}
}

// RequirePortable scans a migration FS for SQL that MariaDB 10.3 rejects. A
// module runs it over the files it ships.
func ExampleRequirePortable() {
	_ = func(t *testing.T, migrations fs.FS) {
		dbtest.RequirePortable(t, migrations)
	}
}

// Portability returns the findings instead of failing a test.
func ExamplePortability() {
	found, _ := dbtest.Portability(fstest.MapFS{
		"20261015000000_queue.sql": {Data: []byte("SELECT id FROM jobs FOR UPDATE SKIP LOCKED;")},
	})

	fmt.Println(found[0])
	// Output: 20261015000000_queue.sql:1: SKIP LOCKED (MariaDB 10.6+)
}
