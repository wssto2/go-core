package order

import "time"

var called = time.Now()

var value = struct{ now func() time.Time }{now: time.Now}
