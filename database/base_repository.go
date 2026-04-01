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
