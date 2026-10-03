package dbtest_test

import (
	"testing"

	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/gorm"
)

// Every target hands out an empty database: a table created by one run is not
// there for the next, and DDL works.
func TestRunGivesEveryTargetAnEmptyDatabase(t *testing.T) {
	for range 2 {
		dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
			if db.Migrator().HasTable("things") {
				t.Fatal("the database is not empty")
			}

			if err := db.Exec("CREATE TABLE things (id INTEGER)").Error; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenReportsAnUnsetTarget(t *testing.T) {
	t.Setenv(dbtest.MariaDB.Env, "")

	if _, ok := dbtest.Open(t, dbtest.MariaDB); ok {
		t.Fatal("want false without a DSN")
	}
}
