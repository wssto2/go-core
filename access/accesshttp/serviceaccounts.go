package accesshttp

import (
	"context"
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
var (
	ServiceAccountBindableRoles = route.Get[BindableInput, BindableRoles](accounts + "/bindable-roles").Name("serviceaccount.bindableroles").
					Requires(serviceaccount.Manage).MalformedPath(http.StatusBadRequest)
	ServiceAccountAccess = route.Get[SubjectInput, SubjectAccess](accounts + "/:id/access").Name("serviceaccount.access").
				Requires(serviceaccount.View).MalformedPath(http.StatusBadRequest)
	ServiceAccountScopes = route.Get[SubjectInput, ScopeOptions](accounts + "/:id/scopes").Name("serviceaccount.scopes").
				Requires(serviceaccount.Manage).MalformedPath(http.StatusBadRequest)
	ServiceAccountBind = route.Post[BindInput, Binding](accounts + "/:id/bindings").Name("serviceaccount.bind").
				Requires(serviceaccount.Manage).Status(http.StatusCreated).MalformedPath(http.StatusBadRequest)
	ServiceAccountUnbind = route.Delete[UnbindInput, route.Empty](accounts + "/:id/bindings/:binding_id").Name("serviceaccount.unbind").
				Requires(serviceaccount.Manage).MalformedPath(http.StatusBadRequest)
)

// ServiceAccountRoutes binds the service accounts' access routes to the bindings service; hand the result to app.Routes.
func ServiceAccountRoutes(bindings *admin.Bindings) []route.Handled {
	h := handlers{bindings: bindings}

	return []route.Handled{
		ServiceAccountBindableRoles.To(h.serviceBindable),
		ServiceAccountAccess.To(h.serviceAccess),
		ServiceAccountScopes.To(h.serviceScopes),
		ServiceAccountBind.To(h.serviceBind),
		ServiceAccountUnbind.To(h.serviceUnbind),
	}
}

func service(id int) authz.Subject { return authz.Subject{Kind: authz.KindServiceAccount, ID: id} }

// positive refuses an id that is zero or negative: no service account has one.
func positive(name string, id int) error {
	if id <= 0 {
		return apperr.BadRequest("invalid " + name + ": must be a positive integer")
	}

	return nil
}

func (h handlers) serviceAccess(ctx context.Context, in SubjectInput) (SubjectAccess, error) {
	if err := positive("id", in.ID); err != nil {
		return SubjectAccess{}, err
	}

	a, err := h.bindings.Access(ctx, service(in.ID))
	if err != nil {
		return SubjectAccess{}, err
	}

	return SubjectAccessOf(a), nil
}

func (h handlers) serviceScopes(ctx context.Context, in SubjectInput) (ScopeOptions, error) {
	if err := positive("id", in.ID); err != nil {
		return ScopeOptions{}, err
	}

	o, err := h.bindings.Scopes(ctx, service(in.ID))
	if err != nil {
		return ScopeOptions{}, err
	}

	return scopesOf(o), nil
}

func (h handlers) serviceBind(ctx context.Context, in BindInput) (Binding, error) {
	if err := positive("id", in.ID); err != nil {
		return Binding{}, err
	}

	scope := authz.Scope{Level: strings.ToLower(strings.TrimSpace(in.Level))}
	if in.ScopeID != nil {
		scope.ID = *in.ScopeID
	}

	b, err := h.bindings.Bind(ctx, service(in.ID), admin.BindingDraft{Role: strings.TrimSpace(in.RoleRef), Scope: scope})
	if err != nil {
		return Binding{}, err
	}

	return BindingOf(b), nil
}

func (h handlers) serviceUnbind(ctx context.Context, in UnbindInput) (route.Empty, error) {
	if err := positive("id", in.ID); err != nil {
		return route.Empty{}, err
	}

	if in.BindingID <= 0 { // the 422 of a path number
		return route.Empty{}, validation.NewValidationError("validation failed",
			map[string][]validation.Failure{"binding_id": {validation.Fail(validation.CodeMustBePositiveNumber)}}, map[string][]string{"binding_id": {"path"}})
	}

	return route.Empty{}, h.bindings.Unbind(ctx, service(in.ID), in.BindingID)
}

func (h handlers) serviceBindable(ctx context.Context, in BindableInput) (BindableRoles, error) {
	scope := authz.Scope{Level: strings.ToLower(strings.TrimSpace(in.Level)), ID: in.ScopeID}

	views, err := h.bindings.BindableRolesFor(ctx, authz.KindServiceAccount, scope)
	if err != nil {
		return BindableRoles{}, err
	}

	out := BindableRoles{Roles: make([]Role, len(views))}
	for i, v := range views {
		out.Roles[i] = RoleOf(v)
	}

	return out, nil
}
