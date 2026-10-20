//nolint:paralleltest,varnamelen,noinlineerr // like the tests of this package
package dbtest_test

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
)

// A repository using WithinTx joins the test transaction, and its write is gone when the test ends.
func TestTxContextIsJoinedByWithinTx(t *testing.T) {
	conn, _ := dbtest.Open(t, dbtest.SQLite)
	if err := conn.Exec("CREATE TABLE things (id INTEGER PRIMARY KEY, name TEXT)").Error; err != nil {
		t.Fatal(err)
	}

	repo := &database.BaseRepository{Conn: conn}

	t.Run("test", func(t *testing.T) {
		ctx, tx := dbtest.TxContext(t, conn)

		if !repo.InTx(ctx) {
			t.Fatal("the repository does not see the test transaction")
		}

		err := repo.WithinTx(ctx, func(ctx context.Context) error {
			if got, _ := database.TxFor(ctx, conn); got != tx {
				t.Error("WithinTx opened its own transaction instead of joining")
			}

			return repo.DB(ctx).Exec("INSERT INTO things VALUES (1, 'x')").Error
		})
		if err != nil {
			t.Fatal(err)
		}

		if n := rows(t, tx); n != 1 {
			t.Fatalf("test transaction has %d rows, want 1", n)
		}
	})

	if n := rows(t, conn); n != 0 {
		t.Fatalf("%d rows survived the test, want 0", n)
	}
}

func rows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM things").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}

	return n
}
