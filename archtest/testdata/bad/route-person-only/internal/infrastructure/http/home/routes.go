package home

import "github.com/wssto2/go-core/route"

const base = "/v1/home"

var (
	Marked   = route.Get[route.None, route.None](base + "/a").Name("a").Requires("p").PersonOnly()
	SignIn   = route.Post[route.None, route.None]("/v1/auth/login").Public()
	Unmarked = route.Get[route.None, route.None](base + "/b").Name("b").Requires("p")
	Search   = route.Raw("GET", "/v1/search").Name("c").To(nil)
	Sibling  = route.Get[route.None, route.None]("/v1/homes").Requires("p")
)
