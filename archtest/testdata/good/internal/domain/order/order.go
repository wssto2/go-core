package order

import (
	"context"

	"example.com/app/internal/domain/shared"
)

type Order struct{ ID shared.ID }

type Repository interface {
	Find(ctx context.Context, id shared.ID) (*Order, error)
	Reader
}

type Reader interface {
	Close(ctx context.Context) error
}
