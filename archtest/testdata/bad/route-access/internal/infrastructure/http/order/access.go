package order

import rt "github.com/wssto2/go-core/route"

var (
	None      = rt.Get[int, int](base).Name("x")
	RawNone   = rt.Raw("POST", base+"/raw").To(nil)
	Chained   = rt.Put[int, int](base).Name("c").Requires("a.b:c").To(nil)
	Person    = rt.Get[int, int](base).PersonOnly()
	Signed    = rt.Get[int, int](base).Name("e").Authenticated().Status(201)
	Any       = rt.Patch[int, int](base).RequiresAny("a.b:c")
	Anonymous = rt.Delete[int, int](base).Public()
)
