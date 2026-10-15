package order

import "example.com/app/internal/domain/customer"

type Injects struct{ repo customer.Repository }
