package database

import (
	"context"

	"gorm.io/gorm"
)

// BaseRepository is the embedded base of a repository on the application's primary connection.
//
// A transaction is opened one way inside a repository: WithinTx. Do not call r.DB(ctx).Transaction(func(tx *gorm.DB)
// ...) there: inside a transaction that call opens a savepoint, and in any case it does not put tx into ctx, so every
// helper the closure calls with ctx still runs on the outer connection, outside the unit of work. WithinTx joins the
// caller's transaction or opens one, and DB(ctx) is that transaction everywhere below it.
type BaseRepository struct {
	Conn *gorm.DB
}

// DB returns the transaction from context if it belongs to Conn, otherwise the base connection.
// Embed this in every repository to get transaction propagation for free.
func (b *BaseRepository) DB(ctx context.Context) *gorm.DB {
	if tx, ok := TxFor(ctx, b.Conn); ok {
		return tx.WithContext(ctx)
	}
	return b.Conn.WithContext(ctx)
}

// InTx reports whether ctx carries a caller's transaction on Conn, that is whether DB(ctx) returns it. A method that
// needs a locking read only when it runs inside its caller's unit of work asks this.
func (b *BaseRepository) InTx(ctx context.Context) bool {
	_, ok := TxFor(ctx, b.Conn)

	return ok
}

// WithinTx runs fn in a transaction: the caller's when ctx carries one on Conn (fn joins it; the caller commits or rolls
// back), otherwise a new one opened on Conn, committed when fn returns nil and rolled back when it returns an error. Inside
// fn, DB(ctx) is that transaction. It is for a method that must be atomic on its own, such as a locked read and the
// write it decides, whether or not its caller holds a transaction.
func (b *BaseRepository) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return NewTransactor(b.Conn).WithinTransaction(ctx, fn) //nolint:wrapcheck // fn's error comes back as it is
}

// PlainRepository is BaseRepository for a connection whose writes must commit on their own even when the surrounding
// business transaction rolls back (a process lock, a job checkpoint). Its DB(ctx) always returns Conn bound to ctx,
// ignoring any transaction in ctx, its own connection's included. A repository merely on another database needs no
// PlainRepository: a BaseRepository joins only a transaction of its own connection.
//
// Choosing PlainRepository over BaseRepository is a decision: say why in the repository's doc comment.
type PlainRepository struct {
	Conn *gorm.DB
}

// DB returns Conn bound to ctx, ignoring any transaction in ctx.
func (p *PlainRepository) DB(ctx context.Context) *gorm.DB {
	return p.Conn.WithContext(ctx)
}
