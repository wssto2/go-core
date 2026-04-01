package sqlc

import (
	"context"

	"github.com/wssto2/go-core/database"
)

// SQLCRepository is a minimal base repository for sqlc-generated Querier implementations.
// It holds a Querier and an optional Transactor used to run operations inside transactions.
type SQLCRepository struct {
	Queries    Querier
	Transactor database.Transactor
}

// NewSQLCRepository constructs a new SQLCRepository.
func NewSQLCRepository(q Querier, t database.Transactor) *SQLCRepository {
	return &SQLCRepository{Queries: q, Transactor: t}
}

// WithinTransaction executes fn within a transaction if a Transactor is provided.
// If the Transactor stored a Querier in the context (via sqlc.WithQuerier), prefer
// that Querier; otherwise fall back to the repository's Queries instance.
func (r *SQLCRepository) WithinTransaction(ctx context.Context, fn func(ctx context.Context, q Querier) error) error {
	if r.Transactor == nil {
		return fn(ctx, r.Queries)
	}
	return r.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		if q, ok := QuerierFromContext(txCtx); ok && q != nil {
			return fn(txCtx, q)
		}
		return fn(txCtx, r.Queries)
	})
}
