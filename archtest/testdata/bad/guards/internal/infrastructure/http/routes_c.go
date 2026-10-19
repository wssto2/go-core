package http

import (
	coreauth "example.com/framework/auth"
	"example.com/framework/tenancy"
)

func c() {
	coreauth.Authenticated()
	tenancy.FromAuthenticatedUser()
}

func d() { tenancy.FromAuthenticatedUser() }
