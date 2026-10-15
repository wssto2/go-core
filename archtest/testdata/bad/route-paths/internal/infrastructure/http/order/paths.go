package order

import "github.com/wssto2/go-core/route"

var (
	Camel   = route.Post[int, int](base + "/sendOrder")
	Snake   = route.Delete[int, int]("/v1/price_rules/:id")
	Upper   = route.Raw("GET", "/v1/Files")
	Wild    = route.Raw("GET", "/v1/files/*path")
	Step    = route.Post[int, int](base + "/stepper/1")
	Mixed   = route.Post[int, int](base + "/stepper/Step1")
	Dynamic = route.Post[int, int](dynamic())
	NotOne  = route.Name("not a route")
)
