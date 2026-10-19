package http

import (
	"example.com/framework/auth"
	"example.com/framework/tenancy"
)

func a() {
	auth.Authenticated()
	tenancy.FromAuthenticatedUser()
}
