package database

import (
	"context"
	"database/sql"
	"fmt"

	"gorm.io/gorm"
)

type txKey struct{}

// txEntry is one open transaction in a context, with the connection (its *sql.DB) it was begun on. Entries chain, so
// a transaction on connection B opened inside one on A leaves A's reachable for the callers that use A.
type txEntry struct {
	pool   *sql.DB
	tx     *gorm.DB
	sqlTx  *sql.Tx
	parent *txEntry
}

func entryFromContext(ctx context.Context) *txEntry {
	e, _ := ctx.Value(txKey{}).(*txEntry)

	return e
}

// find returns the innermost entry on pool. A nil pool (a connection that exposes no *sql.DB) matches nothing.
func (e *txEntry) find(pool *sql.DB) *txEntry {
	for ; e != nil; e = e.parent {
		if pool != nil && e.pool == pool {
			return e
		}
	}

	return nil
}

// poolOf identifies a connection: the *sql.DB under it. A *gorm.DB value is not an identity (WithContext and Session
// return new ones), the registry name is not on the handle, and the *sql.DB is the one thing every handle of a
// connection shares, including the sqlc *sql.Tx's origin.
func poolOf(conn *gorm.DB) *sql.DB {
	if conn == nil {
		return nil
	}

	pool, err := conn.DB()
	if err != nil {
		return nil
	}

	return pool
}

// ContextWithTx returns ctx carrying tx as the open transaction of conn, which tx must have been begun on. Repositories
// on conn join it (WithinTx, BaseRepository.DB); repositories on another connection do not. WithinTransaction uses it;
// a test uses it through dbtest.TxContext.
func ContextWithTx(ctx context.Context, conn, tx *gorm.DB) context.Context {
	entry := &txEntry{pool: poolOf(conn), tx: tx, sqlTx: nil, parent: entryFromContext(ctx)}

	// With PrepareStmt: true (default for MySQL), the ConnPool is a *gorm.PreparedStmtTX that wraps *sql.Tx.
	switch v := tx.Statement.ConnPool.(type) {
	case *sql.Tx:
		entry.sqlTx = v
	case *gorm.PreparedStmtTX:
		entry.sqlTx, _ = v.Tx.(*sql.Tx)
	}

	return context.WithValue(ctx, txKey{}, entry)
}

// TxFromContext retrieves the innermost transaction in the context, whatever connection it belongs to. A repository
// uses TxFor with its own connection instead, so a transaction on another database is never mistaken for its own.
func TxFromContext(ctx context.Context) (*gorm.DB, bool) {
	if e := entryFromContext(ctx); e != nil {
		return e.tx, true
	}

	return nil, false
}

// TxFor retrieves the transaction in the context that belongs to conn, if there is one.
func TxFor(ctx context.Context, conn *gorm.DB) (*gorm.DB, bool) {
	if e := entryFromContext(ctx).find(poolOf(conn)); e != nil {
		return e.tx, true
	}

	return nil, false
}

// SQLTxFromContext retrieves the *sql.Tx of the innermost transaction the Transactor stored in the context.
func SQLTxFromContext(ctx context.Context) (*sql.Tx, bool) {
	if e := entryFromContext(ctx); e != nil && e.sqlTx != nil {
		return e.sqlTx, true
	}

	return nil, false
}

// SQLTxFor is SQLTxFromContext for the transaction that belongs to conn.
func SQLTxFor(ctx context.Context, conn *gorm.DB) (*sql.Tx, bool) {
	if e := entryFromContext(ctx).find(poolOf(conn)); e != nil && e.sqlTx != nil {
		return e.sqlTx, true
	}

	return nil, false
}

// Transactor defines the interface for executing operations within a transaction.
type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type gormTransactor struct {
	conn *gorm.DB
}

// NewTransactor creates a new Transactor instance.
func NewTransactor(conn *gorm.DB) Transactor {
	return &gormTransactor{conn: conn}
}

// WithinTransaction executes the given function within a database transaction.
// If the function returns an error, the transaction is rolled back.
// If the function panics, the transaction is rolled back and the panic is re-raised.
// If the function returns nil, the transaction is committed.
//
// Additionally, when possible this stores the underlying *sql.Tx in the
// context so sqlc-based repositories can construct a Querier bound to that tx.
//
// If ctx already carries a transaction on this connection (started by an outer
// WithinTransaction call, possibly on a different Transactor instance over the same
// database), fn runs directly against it instead of starting a nested transaction —
// nested Begin() calls don't compose into one atomic unit, they'd silently
// open a second, independent transaction on another pooled connection. The
// outermost caller owns commit/rollback. A transaction on another connection is not
// joined: a Transactor on connection B opens its own on B, and the caller's on A stays
// in ctx for the code that uses A.
func (t *gormTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := TxFor(ctx, t.conn); ok {
		return fn(ctx)
	}

	tx := t.conn.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("begin transaction: %w", tx.Error)
	}

	txCtx := ContextWithTx(ctx, t.conn, tx)

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // re-raise so panic recovery middleware can handle it
		}
	}()

	if err := fn(txCtx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// NewTransactorFromRegistry creates a Transactor for the named connection
// in reg. Pass an empty string for dbName to use the primary connection.
func NewTransactorFromRegistry(reg *Registry, dbName string) (Transactor, error) {
	if dbName == "" {
		dbName = reg.PrimaryName()
	}
	conn, err := reg.Get(dbName)
	if err != nil {
		return nil, fmt.Errorf("database: connection %q not found", dbName)
	}
	return NewTransactor(conn), nil
}
