// Package sqlc is a scaffold for integrating SQLC-generated query code with
// the go-core database infrastructure.
//
// SCAFFOLD STATUS: The Querier interface and Queries struct below are
// intentionally empty placeholders. To activate this package:
//  1. Define your SQL queries in database/queries/*.sql
//  2. Run `sqlc generate` from the repository root (see sqlc.yaml)
//  3. Replace the empty Querier interface with the generated sqlc.Querier
//  4. Replace the Queries stub with the generated sqlc.Queries struct
//
// Until those steps are complete, NewFromTx returns an empty Querier that
// cannot execute any real queries.
package sqlc

import (
	"context"
	"database/sql"

	"github.com/wssto2/go-core/database"
)

// Package sqlc provides minimal interfaces and stubs for sqlc-generated code.
// This file introduces the package structure so future sqlc code can be placed under database/sqlc.
//
// Minimal content: Querier interface expected to be implemented by generated *Queries structs.

// Querier is the minimal interface that generated *Queries structs implement.
type Querier interface {
	// Marker interface for sqlc-generated query implementations.
}

// context key for storing a Querier in context.
type querierCtxKey struct{}

// WithQuerier returns a copy of ctx that carries the provided Querier.
func WithQuerier(ctx context.Context, q Querier) context.Context {
	return context.WithValue(ctx, querierCtxKey{}, q)
}

// QuerierFromContext retrieves a Querier from the context if present. If a
// Querier is not directly present, this will attempt to retrieve a *sql.Tx from
// the context (stored by database.Transactor) and construct a Querier using
// NewFromTx.
func QuerierFromContext(ctx context.Context) (Querier, bool) {
	if q, ok := ctx.Value(querierCtxKey{}).(Querier); ok {
		return q, true
	}

	if tx, ok := database.SQLTxFromContext(ctx); ok && tx != nil {
		return NewFromTx(tx), true
	}

	return nil, false
}

// NewFromTx returns a Querier backed by the given SQL transaction.
// NOTE: scaffold implementation — returns an empty Queries struct.
// Replace with the sqlc-generated version after running `sqlc generate`.
func NewFromTx(tx *sql.Tx) Querier {
	return &Queries{} // defined in queries_stub.go
}
