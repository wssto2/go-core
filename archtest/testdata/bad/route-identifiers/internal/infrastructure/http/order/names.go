package order

import "github.com/wssto2/go-core/route"

var (
	Snake  = route.Get[int, int]("/a").Requires("sales_plan.order:view_all")
	Kebab  = route.Group("sales-plan")
	Single = route.Group("salesplan")
	Mixed  = route.Get[int, int]("/b").RequiresAny("ok.fine:view", "x.Y:view")
)
