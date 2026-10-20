package dbtest

import (
	"context"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/wssto2/go-core/database"
)

// Tx returns a transaction on the existing MySQL or MariaDB schema the DSN in
// the environment variable dsnEnv names, rolled back when the test ends. Use it
// for contract tests that must run against a real, already populated schema
// (a legacy database) which Run's throw-away schemas cannot stand in for. The
// test skips when dsnEnv is unset; a set but unusable DSN fails it.
//
//	func TestDealerRepository(t *testing.T) {
//	    tx := dbtest.Tx(t, "MYSQL_TEST_DSN")
//	    repo := NewDealerRepository(tx)
//	    // writes are rolled back
//	}
//
// The DSN gets clientFoundRows=true unless it sets the option itself, as an
// app's own connection does: an UPDATE then reports the rows it matched, not
// only those whose values changed. The test has one connection of its own,
// closed at the end, so a suite does not run the server out of connections.
//
// Only DML is rolled back. DDL (CREATE, ALTER, DROP, TRUNCATE) commits the
// transaction implicitly and its effects stay, as do writes to non-transactional
// tables (MyISAM); delete those by hand in a t.Cleanup.
func Tx(tb testing.TB, dsnEnv string) *gorm.DB {
	tb.Helper()

	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		tb.Skipf("%s is not set: set it to the DSN of the schema to run this test on", dsnEnv)
	}

	conn := openServer(tb, withClientFoundRows(dsn))

	pool, err := conn.DB()
	if err != nil {
		tb.Fatalf("dbtest: %v", err)
	}

	pool.SetMaxOpenConns(1)

	transaction := conn.Begin()
	if transaction.Error != nil {
		tb.Fatalf("dbtest: begin: %v", redact(transaction.Error, dsn))
	}

	// Registered after openServer's close, so it runs first.
	tb.Cleanup(func() { _ = transaction.Rollback().Error })

	return transaction
}

// TxContext begins a transaction on conn, rolled back when the test ends, and returns it with a context that carries it
// as conn's transaction: a repository built on conn joins it (WithinTx, BaseRepository.DB(ctx)), where Tx's bare
// handle is invisible to WithinTx. The handle is the same transaction, for direct queries in the test. A repository
// on another connection does not join it.
//
//	conn := openMySQL(t)
//	ctx, tx := dbtest.TxContext(t, conn)
//	repo := NewDealerRepository(conn)
//	err := repo.Save(ctx, dealer) // inside the test transaction
//
// A read through conn outside ctx needs a second pooled connection. Like Tx, only DML is rolled back.
func TxContext(tb testing.TB, conn *gorm.DB) (context.Context, *gorm.DB) {
	tb.Helper()

	transaction := conn.Begin()
	if transaction.Error != nil {
		tb.Fatalf("dbtest: begin: %v", transaction.Error)
	}

	tb.Cleanup(func() { _ = transaction.Rollback().Error })

	return database.ContextWithTx(context.Background(), conn, transaction), transaction
}

// withClientFoundRows adds clientFoundRows=true to a DSN that does not mention
// the option.
func withClientFoundRows(dsn string) string {
	switch {
	case strings.Contains(dsn, "clientFoundRows="):
		return dsn
	case strings.Contains(dsn, "?"):
		return dsn + "&clientFoundRows=true"
	default:
		return dsn + "?clientFoundRows=true"
	}
}
