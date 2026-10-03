package admin_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access/admin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

// The delegation rules of ARV's access/admin_test.go (IAM-AUTHZ-005), against
// the services: assigning needs the binding permission at a place containing the
// target, forbids a role with a System permission the giver lacks or an
// organization-only one unless the giver manages bindings at the root, never lets
// anyone assign a role to themselves, and nobody loses their own last access.
func TestDelegationRules(t *testing.T) {
	f := newFixture(t)
	f.seed(1, "webmaster", authztest.Org())
	ownBinding := f.seed(2, "dealeradmin", authztest.Dealer(10))

	bind := func(ctx context.Context, subject authz.Subject, role string, scope authz.Scope) error {
		_, err := f.Bindings.Bind(ctx, subject, admin.BindingDraft{Role: role, Scope: scope})
		return err
	}
	target := user(5)

	t.Run("a dealer administrator assigns a role at their dealer, one they do not hold themselves", func(t *testing.T) {
		require.NoError(t, bind(as(2), target, "salesmanager", authztest.Dealer(10)))
		require.NoError(t, f.Engine.RequireOn(authz.WithPrincipal(context.Background(), authz.User(5, 0)), "crm.customer:view", authz.Resource{Scope: authztest.Dealer(10)}))
	})

	t.Run("not at another dealer, not at the organization", func(t *testing.T) {
		require.ErrorIs(t, bind(as(2), user(6), "salesmanager", authztest.Dealer(20)), authz.ErrForbidden)
		require.ErrorIs(t, bind(as(2), user(6), "salesmanager", authztest.Org()), authz.ErrForbidden)
	})

	t.Run("nobody assigns a role to themselves", func(t *testing.T) {
		require.ErrorIs(t, bind(as(2), user(2), "salesmanager", authztest.Dealer(10)), authz.ErrSelfAssignment)
		require.ErrorIs(t, bind(as(1), user(1), "salesmanager", authztest.Org()), authz.ErrSelfAssignment)
	})

	t.Run("organization-only permissions are bound at the organization only, by someone who manages bindings there", func(t *testing.T) {
		require.Error(t, bind(as(2), target, "distribution", authztest.Dealer(10)))
		require.NoError(t, bind(as(1), user(7), "distribution", authztest.Org()))
		require.Error(t, bind(as(1), user(7), "distribution", authztest.Dealer(10)), "not even the webmaster below the organization")
	})

	t.Run("a System permission is only given by someone who holds it", func(t *testing.T) {
		system, err := f.Roles.Create(as(1), admin.RoleDraft{Name: "System", Grants: authz.Grants(authz.QualifierAll, "system.job:run")})
		require.NoError(t, err)

		require.ErrorIs(t, bind(as(2), target, system.Ref, authztest.Dealer(10)), authz.ErrEscalation)
		require.NoError(t, bind(as(1), user(8), system.Ref, authztest.Dealer(10)))
		require.Error(t, bind(as(2), target, "webmaster", authztest.Dealer(10)), "the computed webmaster is refused below the organization outright")
	})

	t.Run("nobody removes their own last access to binding management", func(t *testing.T) {
		require.ErrorIs(t, f.Bindings.Unbind(as(2), user(2), ownBinding.ID), authz.ErrLastAdmin)
	})

	t.Run("editing roles needs the role permission, which a dealer administrator lacks", func(t *testing.T) {
		draft := admin.RoleDraft{Name: "Mine", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")}
		_, err := f.Roles.Create(as(2), draft)
		require.ErrorIs(t, err, authz.ErrForbidden)

		saved, err := f.Roles.Create(as(1), draft)
		require.NoError(t, err)
		assert.NotEmpty(t, saved.Ref)
	})

	t.Run("a role cannot hold more than its author", func(t *testing.T) {
		f.seed(3, "roleadmin", authztest.Org())
		_, err := f.Roles.Create(as(3), admin.RoleDraft{Name: "Too much", Grants: authz.Grants(authz.QualifierAll, "system.job:run")})
		require.ErrorIs(t, err, authz.ErrEscalation)
	})
}

// A subject with two bindings that both grant the protected permission may
// remove either, never both at once, even when both removals are in flight.
func TestConcurrentLastAdminRemoval(t *testing.T) {
	onEveryDatabase(t, func(t *testing.T, f *fixture) {
		f.seed(1, "webmaster", authztest.Org())

		first := f.seed(2, "dealeradmin", authztest.Dealer(10))
		second := f.seed(2, "dealeradmin", authztest.Dealer(20))

		var wg sync.WaitGroup

		errs := make([]error, 2)
		for i, id := range []int{first.ID, second.ID} {
			wg.Go(func() { errs[i] = f.Bindings.Unbind(as(2), user(2), id) })
		}

		wg.Wait()

		won, refused := 0, 0

		for _, err := range errs {
			switch {
			case err == nil:
				won++
			case errors.Is(err, authz.ErrLastAdmin):
				refused++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}

		assert.Equal(t, [2]int{1, 1}, [2]int{won, refused}, "one removal goes through, the other would leave nothing")

		left, err := f.Store.BindingsFor(context.Background(), user(2))
		require.NoError(t, err)
		assert.Len(t, left, 1)
	})
}

// Many identical binds at once: exactly one wins, the rest are refused as duplicates.
func TestConcurrentBinds(t *testing.T) {
	onEveryDatabase(t, func(t *testing.T, f *fixture) {
		f.seed(1, "webmaster", authztest.Org())

		const n = 8

		var wg sync.WaitGroup

		errs := make([]error, n)
		for i := range n {
			wg.Go(func() {
				_, errs[i] = f.Bindings.Bind(as(1), user(5), admin.BindingDraft{Role: "seller", Scope: authztest.Dealer(10)})
			})
		}

		wg.Wait()

		won := 0

		for _, err := range errs {
			if err == nil {
				won++
				continue
			}

			assert.ErrorIs(t, err, authz.ErrDuplicateBinding)
		}

		assert.Equal(t, 1, won)
	})
}
