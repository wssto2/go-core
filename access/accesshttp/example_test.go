package accesshttp_test

import (
	"fmt"

	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/access/admin"
)

// Declare declares the routes under /v1, guarded by the module's permissions;
// nothing is installed or opened. gocore.WithPrefix("/api") serves them at
// /api/v1/...
func ExampleDeclare() {
	routes := accesshttp.Declare()

	for _, spec := range routes.Contract().Specs()[:3] {
		fmt.Println(spec.Method, spec.Path, spec.Permission)
	}
	// Output:
	// GET /v1/iam/roles iam.role:view
	// GET /v1/iam/roles/:ref iam.role:view
	// GET /v1/iam/roles/:ref/holders iam.role:view
}

// Contract is what the TypeScript generator reads: the whole HTTP surface of the
// module, as a group named access.
func ExampleRoutes_Contract() {
	group := accesshttp.Declare().Contract()

	fmt.Println(group.Name(), len(group.Specs()), "routes")
	// Output: access 14 routes
}

// To binds the services to the declared routes; access.Install does it and hands
// the result to app.Routes.
func ExampleRoutes_To() {
	routes := accesshttp.Declare()

	var roles *admin.Roles // built by access.Install

	bound := routes.To(roles, nil, nil)
	fmt.Println(len(bound), "handlers")
	// Output: 14 handlers
}
