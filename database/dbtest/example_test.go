package dbtest_test

import (
	"testing"

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
