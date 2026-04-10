package database

import (
	"context"
	"database/sql"
	"fmt"

	"gorm.io/gorm"
)

type txKey struct{}
type sqlTxKey struct{}

// TxFromContext retrieves the transaction from the context if it exists.
func TxFromContext(ctx context.Context) (*gorm.DB, bool) {
	tx, ok := ctx.Value(txKey{}).(*gorm.DB)
	return tx, ok
}

// SQLTxFromContext retrieves the underlying *sql.Tx stored in the context by the Transactor.
func SQLTxFromContext(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(sqlTxKey{}).(*sql.Tx)
	return tx, ok
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
// If the function returns nil, the transaction is committed.
//
// Additionally, when possible this stores the underlying *sql.Tx in the
// context so sqlc-based repositories can construct a Querier bound to that tx.
func (t *gormTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return t.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Store the tx in context so repositories can pick it up
		txCtx := context.WithValue(ctx, txKey{}, tx)

		// Attempt to extract the underlying *sql.Tx from GORM's connection pool and
		// store it in the context for consumers (sqlc package, audit repo) to use.
		// With PrepareStmt: true (default for MySQL), the ConnPool is a
		// *gorm.PreparedStmtTX that wraps *sql.Tx — handle both.
		if cp := tx.Statement.ConnPool; cp != nil {
			switch v := cp.(type) {
			case *sql.Tx:
				txCtx = context.WithValue(txCtx, sqlTxKey{}, v)
			case *gorm.PreparedStmtTX:
				if sqlTx, ok := v.Tx.(*sql.Tx); ok {
					txCtx = context.WithValue(txCtx, sqlTxKey{}, sqlTx)
				}
			}
		}

		return fn(txCtx)
	})
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
