//nolint:paralleltest,tparallel,thelper,noinlineerr,varnamelen // t.Setenv rules out t.Parallel
package dbtest_test

import (
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/wssto2/go-core/database/dbtest"
)

// existingSchema is a MySQL or MariaDB schema holding table things(id, name)
// with one row, and DSNEnv names a variable that holds the DSN of that schema,
// the way an app's contract tests point at an existing database.
func existingSchema(t *testing.T, db *gorm.DB) string {
	t.Helper()

	for _, stmt := range []string{
		"CREATE TABLE things (id INT PRIMARY KEY, name VARCHAR(20))",
		"INSERT INTO things VALUES (1, 'kept')",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}

	dialector, ok := db.Dialector.(*mysql.Dialector)
	if !ok {
		t.Fatal("not a MySQL connection")
	}

	t.Setenv("GOCORE_TX_TEST_DSN", dialector.DSNConfig.FormatDSN())

	return "GOCORE_TX_TEST_DSN"
}

func TestTxSkipsWithoutADSN(t *testing.T) {
	t.Setenv("GOCORE_TX_UNSET", "")

	t.Run("unset", func(t *testing.T) {
		dbtest.Tx(t, "GOCORE_TX_UNSET")
		t.Fatal("Tx returned instead of skipping")
	})
}

// A write inside the transaction is visible there and gone once the test ends.
func TestTxRollsBackWhenTheTestEnds(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, scratch *gorm.DB) {
		env := existingSchema(t, scratch)

		t.Run("test", func(t *testing.T) {
			tx := dbtest.Tx(t, env)

			if err := tx.Exec("INSERT INTO things VALUES (2, 'rolled back')").Error; err != nil {
				t.Fatal(err)
			}

			if n := count(t, tx); n != 2 {
				t.Fatalf("inside the transaction: %d rows, want 2", n)
			}
		})

		if n := count(t, scratch); n != 1 {
			t.Fatalf("after the test: %d rows, want 1 (the insert was committed)", n)
		}
	}, dbtest.MySQL, dbtest.MariaDB)
}

// An UPDATE that changes nothing still reports the row it matched, as the
// app's own connection does; an explicit clientFoundRows in the DSN wins.
func TestTxReportsMatchedRows(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, scratch *gorm.DB) {
		env := existingSchema(t, scratch)

		t.Run("default", func(t *testing.T) {
			tx := dbtest.Tx(t, env)

			res := tx.Exec("UPDATE things SET name = 'kept' WHERE id = 1")
			if res.Error != nil || res.RowsAffected != 1 {
				t.Fatalf("matched rows = %d (err %v), want 1", res.RowsAffected, res.Error)
			}
		})

		t.Run("explicit", func(t *testing.T) {
			t.Setenv(env, os.Getenv(env)+"&clientFoundRows=false")
			tx := dbtest.Tx(t, env)

			res := tx.Exec("UPDATE things SET name = 'kept' WHERE id = 1")
			if res.Error != nil || res.RowsAffected != 0 {
				t.Fatalf("changed rows = %d (err %v), want 0", res.RowsAffected, res.Error)
			}
		})
	}, dbtest.MySQL, dbtest.MariaDB)
}

func count(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var n int64

	if err := db.Raw("SELECT COUNT(*) FROM things").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}

	return n
}
