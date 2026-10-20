package ledger

import "context"

type Entry struct{ ID int }

type Repository interface {
	Find(ctx context.Context, id int) (*Entry, error)
	Create(ctx context.Context, e *Entry) error
	Delete(ctx context.Context, id int) error
}

type BalanceRepository interface {
	UpdateBalance(ctx context.Context, id int) error
}
