package accesshttp_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/identity/serviceaccount"
)

const robots = "/v1/iam/service-accounts"

// The access of a service account is the access of a person, with the subject kind "service": the same
// delegation rules, under the service accounts' path and permissions.
func TestServiceAccountAccessOverHTTP(t *testing.T) {
	s := newServer(t, "")
	s.seed(1, "webmaster", authztest.Org())
	s.seed(2, "admin", authztest.Org()) // holds iam.user:*, not iam.serviceaccount:*

	t.Run("a role is given, shown and taken away", func(t *testing.T) {
		bound := s.do(1, "POST", robots+"/5/bindings", accesshttp.BindInput{RoleRef: "seller", Level: "dealer", ScopeID: new(10)})
		require.Equal(t, http.StatusCreated, bound.Status, string(bound.Data))

		binding := decode[accesshttp.Binding](t, bound)
		assert.Equal(t, "seller", binding.Role.Ref)

		access := decode[accesshttp.SubjectAccess](t, s.do(1, "GET", robots+"/5/access", nil))
		assert.Equal(t, accesshttp.SubjectRef{Kind: authz.KindServiceAccount, ID: 5}, access.Subject)
		require.Len(t, access.Bindings, 1)
		assert.True(t, access.CanManage)

		stored, err := s.store.BindingsFor(t.Context(), authz.Subject{Kind: authz.KindServiceAccount, ID: 5})
		require.NoError(t, err)
		assert.Len(t, stored, 1, "the binding is the service account's, not user 5's")

		person, err := s.store.BindingsFor(t.Context(), authz.Subject{Kind: authz.KindUser, ID: 5})
		require.NoError(t, err)
		assert.Empty(t, person)

		scopes := s.do(1, "GET", robots+"/5/scopes", nil)
		assert.Equal(t, http.StatusOK, scopes.Status, string(scopes.Data))

		assert.Equal(t, http.StatusNoContent, s.do(1, "DELETE", robots+"/5/bindings/"+strconv.Itoa(binding.ID), nil).Status)
	})

	t.Run("the roles that can be given at a place", func(t *testing.T) {
		roles := s.do(1, "GET", robots+"/bindable-roles?level=dealer&scope_id=10", nil)
		require.Equal(t, http.StatusOK, roles.Status, string(roles.Data))
		assert.NotEmpty(t, decode[accesshttp.BindableRoles](t, roles).Roles)
	})

	t.Run("an account that does not exist is not found", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, s.do(1, "GET", robots+"/99/access", nil).Status)
		assert.Equal(t, http.StatusNotFound, s.do(1, "POST", robots+"/99/bindings",
			accesshttp.BindInput{RoleRef: "seller", Level: "dealer", ScopeID: new(10)}).Status)
	})

	t.Run("a person's permissions are not a service account's", func(t *testing.T) {
		for _, c := range []struct{ method, path string }{
			{"GET", robots + "/5/access"}, {"GET", robots + "/5/scopes"}, {"POST", robots + "/5/bindings"},
			{"DELETE", robots + "/5/bindings/1"}, {"GET", robots + "/bindable-roles?level=organization"},
		} {
			assert.Equal(t, http.StatusForbidden, s.do(2, c.method, c.path, nil).Status, c.method+" "+c.path+": iam.user:manage is not iam.serviceaccount:manage")
		}

		assert.Equal(t, http.StatusUnauthorized, s.do(0, "GET", robots+"/5/access", nil).Status)
	})

	t.Run("a path id that is no id is told so", func(t *testing.T) {
		for _, path := range []string{"/abc/access", "/0/access", "/-1/scopes", "/1.5/access"} {
			assert.Equal(t, http.StatusBadRequest, s.do(1, "GET", robots+path, nil).Status, path)
		}

		assert.Equal(t, http.StatusUnprocessableEntity, s.do(1, "DELETE", robots+"/5/bindings/0", nil).Status, "a binding id of zero")
		assert.Equal(t, http.StatusBadRequest, s.do(1, "DELETE", robots+"/0/bindings/1", nil).Status)
	})
}

// A service account's access is guarded by iam.serviceaccount:*, a person's by iam.user:*: each way round, the one
// permission does not open the other's.
func TestTheAccessOfEachKindOfSubjectHasItsOwnPermissions(t *testing.T) {
	s := newServer(t, "")
	s.seed(1, "webmaster", authztest.Org())
	s.seed(3, "robots", authztest.Org()) // iam.serviceaccount:view and :manage, nothing of iam.user:*

	give := accesshttp.BindInput{RoleRef: "seller", Level: "dealer", ScopeID: new(10)}

	t.Run("who manages service accounts gives a role to one", func(t *testing.T) {
		bound := s.do(3, "POST", robots+"/5/bindings", give)
		require.Equal(t, http.StatusCreated, bound.Status, string(bound.Data))

		assert.Equal(t, http.StatusOK, s.do(3, "GET", robots+"/5/access", nil).Status)
		assert.Equal(t, http.StatusOK, s.do(3, "GET", robots+"/5/scopes", nil).Status)

		roles := s.do(3, "GET", robots+"/bindable-roles?level=dealer&scope_id=10", nil)
		require.Equal(t, http.StatusOK, roles.Status, string(roles.Data))

		refs := []string{}
		for _, role := range decode[accesshttp.BindableRoles](t, roles).Roles {
			refs = append(refs, role.Ref)
		}

		assert.Contains(t, refs, "seller")

		binding := decode[accesshttp.Binding](t, bound)
		assert.Equal(t, http.StatusNoContent, s.do(3, "DELETE", robots+"/5/bindings/"+strconv.Itoa(binding.ID), nil).Status)
	})

	t.Run("and cannot touch a person's", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, s.do(3, "POST", "/v1/iam/users/5/bindings", give).Status)
		assert.Equal(t, http.StatusForbidden, s.do(3, "GET", "/v1/iam/users/5/access", nil).Status)
		assert.Equal(t, http.StatusForbidden, s.do(3, "GET", "/v1/iam/users/5/scopes", nil).Status)

		roles := s.do(3, "GET", "/v1/iam/bindable-roles?level=dealer&scope_id=10", nil)
		require.Equal(t, http.StatusOK, roles.Status)
		assert.Empty(t, decode[accesshttp.BindableRoles](t, roles).Roles, "none a person may be given by somebody who gives none")
	})

	t.Run("who manages people cannot touch a service account's", func(t *testing.T) {
		s.seed(2, "admin", authztest.Org())

		assert.Equal(t, http.StatusForbidden, s.do(2, "POST", robots+"/5/bindings", give).Status)
		assert.Equal(t, http.StatusForbidden, s.do(2, "GET", robots+"/5/access", nil).Status)
		assert.Equal(t, http.StatusOK, s.do(1, "POST", "/v1/iam/users/5/bindings", give).Status, "the webmaster holds both")
	})
}

// The permission ids the service layer asks are the ones the module's routes and catalogue define.
func TestTheServiceAccountPermissionIDsAgree(t *testing.T) {
	assert.Equal(t, serviceaccount.View, admin.ViewServiceAccountAccess)
	assert.Equal(t, serviceaccount.Manage, admin.ManageServiceAccountBindings)
}
