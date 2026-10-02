// Package route declares HTTP routes as typed values.
//
// A route is declared once, with its method, path, input and output types and
// the permission it needs. The declaration is a plain package-level value: it
// can be read without a running application (to generate the TypeScript
// contract, for example). The behaviour is a plain function the compiler ties
// to the declaration.
//
//	var (
//	    Show   = route.Get[ShowInput, Ticket]("/tickets/:id").Requires(PermView)
//	    Routes = route.Group("tickets", Show)
//	)
//
//	func (s *Service) Show(ctx context.Context, in ShowInput) (Ticket, error) { ... }
//
//	app.Routes(Show.To(service.Show)) // a function of another shape does not compile
//
// go-core binds the path, the query string and the body into the input,
// validates it, checks the permission, calls the function and writes the
// response with the usual envelope and apperr mapping.
//
// Input structs name where each field comes from: `path:"id"` for a URL
// parameter, `query:"page"` for a query-string value, `json:"title"` (or
// `form`) for the body. Use None for a route without input and Empty for a
// route that answers 204 No Content.
package route
