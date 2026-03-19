package database

import (
	"context"

	"gorm.io/gorm"
)

type txKey struct{}

// TxFromContext retrieves the transaction from the context if it exists.
func TxFromContext(ctx context.Context) (*gorm.DB, bool) {
	tx, ok := ctx.Value(txKey{}).(*gorm.DB)
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
func (t *gormTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return t.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Store the tx in context so repositories can pick it up
		txCtx := context.WithValue(ctx, txKey{}, tx)
		return fn(txCtx)
	})
}
