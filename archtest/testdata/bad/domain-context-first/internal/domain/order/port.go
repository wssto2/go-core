package order

import "context"

type Port interface {
	Get(id int) error
	OK(ctx context.Context, id int)
}
