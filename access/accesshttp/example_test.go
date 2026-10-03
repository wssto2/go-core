package accesshttp_test

import (
	"fmt"

	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/access/admin"
)

// Declare puts the routes under a prefix and guards them with the
// application's permissions; nothing is installed or opened.
func ExampleDeclare() {
	routes := accesshttp.Declare("/api/v1", admin.DefaultPermissions)

	for _, spec := range routes.Contract().Specs()[:3] {
		fmt.Println(spec.Method, spec.Path, spec.Permission)
	}
	// Output:
	// GET /api/v1/iam/roles iam.role:view
	// GET /api/v1/iam/roles/:ref iam.role:view
	// GET /api/v1/iam/roles/:ref/holders iam.role:view
}

// Contract is what the TypeScript generator reads: the whole HTTP surface of the
// module, as a group named access.
func ExampleRoutes_Contract() {
	group := accesshttp.Declare("", admin.DefaultPermissions).Contract()

	fmt.Println(group.Name(), len(group.Specs()), "routes")
	// Output: access 14 routes
}

// To binds the services to the declared routes; access.Install does it and hands
// the result to app.Routes.
func ExampleRoutes_To() {
	routes := accesshttp.Declare("", admin.DefaultPermissions)

	var roles *admin.Roles // built by access.Install

	bound := routes.To(roles, nil, nil)
	fmt.Println(len(bound), "handlers")
	// Output: 14 handlers
}
