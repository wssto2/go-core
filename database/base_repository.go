package database

import (
	"context"

	"gorm.io/gorm"
)

type BaseRepository struct {
	Conn *gorm.DB
}

// DB returns the transaction from context if present, otherwise the base connection.
// Embed this in every repository to get transaction propagation for free.
func (b *BaseRepository) DB(ctx context.Context) *gorm.DB {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.WithContext(ctx)
	}
	return b.Conn.WithContext(ctx)
}

// InTx reports whether ctx carries a caller's transaction, that is whether DB(ctx) returns it. A method that needs a
// locking read only when it runs inside its caller's unit of work asks this.
func (b *BaseRepository) InTx(ctx context.Context) bool {
	_, ok := TxFromContext(ctx)

	return ok
}

// WithinTx runs fn in a transaction: the caller's when ctx carries one (fn joins it; the caller commits or rolls back),
// otherwise a new one opened on Conn, committed when fn returns nil and rolled back when it returns an error. Inside
// fn, DB(ctx) is that transaction. It is for a method that must be atomic on its own, such as a locked read and the
// write it decides, whether or not its caller holds a transaction.
func (b *BaseRepository) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return NewTransactor(b.Conn).WithinTransaction(ctx, fn)
}

// PlainRepository is BaseRepository for a connection the caller's transaction must never reach: a repository on
// another database than the one the Transactor opens its transactions on (a transaction in ctx belongs to that
// database, so using it would run the query against the wrong schema), or one whose writes must commit on their own
// even when the surrounding business transaction rolls back (a process lock, a job checkpoint). Its DB(ctx) always
// returns Conn bound to ctx.
//
// Choosing PlainRepository over BaseRepository is a decision: say why in the repository's doc comment.
type PlainRepository struct {
	Conn *gorm.DB
}

// DB returns Conn bound to ctx, ignoring any transaction in ctx.
func (p *PlainRepository) DB(ctx context.Context) *gorm.DB {
	return p.Conn.WithContext(ctx)
}
