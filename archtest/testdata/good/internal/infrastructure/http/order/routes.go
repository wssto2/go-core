package order

import (
	"example.com/app/internal/application/order"
	"github.com/wssto2/go-core/route"
)

const base = "/v1/orders"

var (
	Create = route.Post[order.CreateInput, route.None](base).Requires("sales.order:create")
	Show   = route.Get[route.None, route.None](base + "/:id/send-order").Authenticated()
	Health = route.Raw("GET", "/health").Public()
	_      = route.Group("order", Create, Show)
)
