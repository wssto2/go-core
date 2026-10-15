package customer

import "context"

type Customer struct{}

type Repository interface{ Find(ctx context.Context) }
