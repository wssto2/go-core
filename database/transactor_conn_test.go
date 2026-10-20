//nolint:paralleltest,testpackage,varnamelen,noinlineerr // internal test like transactor_tx_test.go
package database

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openItems opens a private in-memory database with an items table.
func openItems(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}

	pool, _ := db.DB()
	pool.SetMaxOpenConns(1) // an in-memory SQLite database is private to one connection
	t.Cleanup(func() { _ = pool.Close() })

	if err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}

	return db
}

func countItems(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM items").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}

	return n
}

var errRollback = errors.New("rollback")

// A repository on connection B, called inside a transaction on A, writes on B and opens its own transaction there:
// A's rollback does not undo it.
func TestTransactionOfAnotherConnectionIsNotJoined(t *testing.T) {
	a, b := openItems(t), openItems(t)
	repoA, repoB := &BaseRepository{Conn: a}, &BaseRepository{Conn: b}

	err := repoA.WithinTx(context.Background(), func(ctx context.Context) error {
		if err := repoA.DB(ctx).Exec("INSERT INTO items(name) VALUES ('a')").Error; err != nil {
			return err
		}

		if repoB.InTx(ctx) {
			t.Error("B sees A's transaction")
		}

		if err := repoB.WithinTx(ctx, func(ctx context.Context) error {
			return repoB.DB(ctx).Exec("INSERT INTO items(name) VALUES ('b')").Error
		}); err != nil {
			return err
		}

		// A's transaction is still the one A's callers get.
		if !repoA.InTx(ctx) {
			t.Error("A lost its transaction")
		}

		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("got %v", err)
	}

	if n := countItems(t, b); n != 1 {
		t.Errorf("B has %d rows, want 1 (committed on its own)", n)
	}

	if n := countItems(t, a); n != 0 {
		t.Errorf("A has %d rows, want 0 (rolled back)", n)
	}
}

// Inside A's transaction, a repository on A joins it: it sees the uncommitted row, and a nested
// WithinTx on A joins too.
func TestTransactionOfTheSameConnectionIsJoined(t *testing.T) {
	a := openItems(t)
	repoA := &BaseRepository{Conn: a}

	err := repoA.WithinTx(context.Background(), func(ctx context.Context) error {
		if err := repoA.DB(ctx).Exec("INSERT INTO items(name) VALUES ('a')").Error; err != nil {
			return err
		}

		// A second Transactor over the same connection joins as well (the single pooled connection would deadlock if not).
		return NewTransactor(a).WithinTransaction(ctx, func(ctx context.Context) error {
			var n int64
			if err := repoA.DB(ctx).Raw("SELECT COUNT(*) FROM items").Scan(&n).Error; err != nil {
				return err
			}

			if n != 1 {
				t.Errorf("saw %d rows, want the uncommitted 1", n)
			}

			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The sqlc *sql.Tx follows the same rule.
func TestSQLTxIsPerConnection(t *testing.T) {
	a, b := openItems(t), openItems(t)

	err := NewTransactor(a).WithinTransaction(context.Background(), func(ctx context.Context) error {
		if _, ok := SQLTxFor(ctx, a); !ok {
			t.Error("A has no sql tx")
		}

		if _, ok := SQLTxFor(ctx, b); ok {
			t.Error("B got A's sql tx")
		}

		return NewTransactor(b).WithinTransaction(ctx, func(ctx context.Context) error {
			sa, _ := SQLTxFor(ctx, a)
			sb, _ := SQLTxFor(ctx, b)

			if sa == nil || sb == nil || sa == sb {
				t.Error("each connection should keep its own sql tx")
			}

			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}
