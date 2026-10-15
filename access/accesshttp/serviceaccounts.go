package accesshttp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/identity/serviceaccount"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/validation"
)

const accounts = base + "/iam/service-accounts"

// The access routes of a service account: the same handlers and answers as a person's (SubjectAccess,
// SubjectScopes, Bind, Unbind, BindableRoles), under the service accounts' path and guarded by their
// permissions: reading is iam.serviceaccount:view, changing (and the places to change at) :manage. They are
// served by access.Install when identity has service accounts (WithServiceAccounts), and listed in the
// contract of the service accounts (serviceaccount/http.Contract), whose path ids answer 400 when malformed.
//
//nolint:gochecknoglobals // a route is declared once, as a value
var (
	ServiceAccountBindableRoles = route.Get[BindableInput, BindableRoles](accounts + "/bindable-roles").
					Name("serviceaccount.bindableroles").Requires(serviceaccount.Manage).MalformedPath(http.StatusBadRequest)
	ServiceAccountAccess = route.Get[SubjectInput, SubjectAccess](accounts + "/:id/access").
				Name("serviceaccount.access").Requires(serviceaccount.View).MalformedPath(http.StatusBadRequest)
	ServiceAccountScopes = route.Get[SubjectInput, ScopeOptions](accounts + "/:id/scopes").
				Name("serviceaccount.scopes").Requires(serviceaccount.Manage).MalformedPath(http.StatusBadRequest)
	ServiceAccountBind = route.Post[BindInput, Binding](accounts + "/:id/bindings").
				Name("serviceaccount.bind").Requires(serviceaccount.Manage).Status(http.StatusCreated).
				MalformedPath(http.StatusBadRequest)
	ServiceAccountUnbind = route.Delete[UnbindInput, route.Empty](accounts + "/:id/bindings/:binding_id").
				Name("serviceaccount.unbind").Requires(serviceaccount.Manage).MalformedPath(http.StatusBadRequest)
)

// ServiceAccountRoutes binds the service accounts' access routes to the bindings service; hand the result to
// app.Routes.
func ServiceAccountRoutes(bindings *admin.Bindings) []route.Handled {
	handler := handlers{roles: nil, bindings: bindings}

	return []route.Handled{
		ServiceAccountBindableRoles.To(handler.serviceBindable),
		ServiceAccountAccess.To(handler.serviceAccess),
		ServiceAccountScopes.To(handler.serviceScopes),
		ServiceAccountBind.To(handler.serviceBind),
		ServiceAccountUnbind.To(handler.serviceUnbind),
	}
}

func service(id int) authz.Subject { return authz.Subject{Kind: authz.KindServiceAccount, ID: id} }

// positive refuses an id that is zero or negative: no service account has one.
func positive(id int) error {
	if id <= 0 {
		return apperr.BadRequest("invalid id: must be a positive integer")
	}

	return nil
}

func (h handlers) serviceAccess(ctx context.Context, input SubjectInput) (SubjectAccess, error) {
	err := positive(input.ID)
	if err != nil {
		return SubjectAccess{}, err
	}

	access, err := h.bindings.Access(ctx, service(input.ID))
	if err != nil {
		return SubjectAccess{}, fmt.Errorf("accesshttp: service account access: %w", err)
	}

	return SubjectAccessOf(access), nil
}

func (h handlers) serviceScopes(ctx context.Context, input SubjectInput) (ScopeOptions, error) {
	err := positive(input.ID)
	if err != nil {
		return ScopeOptions{}, err
	}

	options, err := h.bindings.Scopes(ctx, service(input.ID))
	if err != nil {
		return ScopeOptions{}, fmt.Errorf("accesshttp: service account scopes: %w", err)
	}

	return scopesOf(options), nil
}

func (h handlers) serviceBind(ctx context.Context, input BindInput) (Binding, error) {
	err := positive(input.ID)
	if err != nil {
		return Binding{}, err
	}

	scope := authz.Scope{Level: strings.ToLower(strings.TrimSpace(input.Level)), ID: 0}
	if input.ScopeID != nil {
		scope.ID = *input.ScopeID
	}

	draft := admin.BindingDraft{Role: strings.TrimSpace(input.RoleRef), Scope: scope}

	bound, err := h.bindings.Bind(ctx, service(input.ID), draft)
	if err != nil {
		return Binding{}, fmt.Errorf("accesshttp: service account bind: %w", err)
	}

	return BindingOf(bound), nil
}

func (h handlers) serviceUnbind(ctx context.Context, input UnbindInput) (route.Empty, error) {
	err := positive(input.ID)
	if err != nil {
		return route.Empty{}, err
	}

	if input.BindingID <= 0 { // the 422 of a path number
		return route.Empty{}, validation.NewValidationError("validation failed",
			map[string][]validation.Failure{"binding_id": {validation.Fail(validation.CodeMustBePositiveNumber)}},
			map[string][]string{"binding_id": {"path"}})
	}

	err = h.bindings.Unbind(ctx, service(input.ID), input.BindingID)
	if err != nil {
		return route.Empty{}, fmt.Errorf("accesshttp: service account unbind: %w", err)
	}

	return route.Empty{}, nil
}

func (h handlers) serviceBindable(ctx context.Context, input BindableInput) (BindableRoles, error) {
	scope := authz.Scope{Level: strings.ToLower(strings.TrimSpace(input.Level)), ID: input.ScopeID}

	views, err := h.bindings.BindableRolesFor(ctx, authz.KindServiceAccount, scope)
	if err != nil {
		return BindableRoles{}, fmt.Errorf("accesshttp: service account bindable roles: %w", err)
	}

	out := BindableRoles{Roles: make([]Role, len(views))}
	for i, v := range views {
		out.Roles[i] = RoleOf(v)
	}

	return out, nil
}
