package sqlc

// Queries is a placeholder. Replace with the sqlc-generated Queries struct
// after running `sqlc generate`. See package doc in sqlc.go.
type Queries struct{}

// Enforce that Queries implements Querier at compile time.
var _ Querier = (*Queries)(nil)
