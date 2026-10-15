//nolint:exhaustruct,exhaustruct_v5,lll,paralleltest,varnamelen,wrapcheck // tests in the style of the package's others
package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

func TestHoldersOfAPermission(t *testing.T) {
	w := newSmallWorld(t)
	ctx := context.Background()
	robot := authz.Subject{Kind: authz.KindServiceAccount, ID: 9}

	custom := w.SaveRole(authz.Role{Name: "Lead", Grants: authz.Grants(authz.QualifierAll, "crm.lead:view", "crm.lead:assign")})
	reading := w.SaveRole(authz.Role{Name: "Reading", Grants: authz.Grants(authz.QualifierAll, "crm.lead:view")})

	w.Bind(user(1), "manager", authztest.Dealer(1))        // predefined, grants it
	w.Bind(user(2), "seller", authztest.Dealer(1))         // predefined, does not
	w.BindCustom(user(3), custom.ID, authztest.Dealer(2))  // custom, grants it
	w.BindCustom(user(4), reading.ID, authztest.Dealer(1)) // custom, does not
	w.Bind(robot, "dealerlead", authztest.Dealer(1))       // a service account, returned with its kind
	w.Bind(user(5), "webmaster", authz.Scope{})            // a computed role grants everything
	w.Bind(user(1), "dealerlead", authztest.Dealer(2))     // twice, once in the answer
	w.Bind(user(3), "manager", authztest.Dealer(1))        // a second role of the same person

	got, err := w.Engine.HoldersOf(ctx, "crm.lead:assign")
	require.NoError(t, err)
	assert.Equal(t, []authz.Subject{user(1), user(3), robot, user(5)}, got, "each subject once, in the order of their first binding")

	got, err = w.Engine.HoldersOf(ctx, "vehicle.stock:view")
	require.NoError(t, err)
	assert.ElementsMatch(t, []authz.Subject{user(2), user(5)}, got, "seller and the computed webmaster")

	got, err = w.Engine.HoldersOf(ctx, "report.group:view")
	require.NoError(t, err)
	assert.Equal(t, []authz.Subject{user(5)}, got)

	none, err := w.Engine.HoldersOf(ctx, "system.job:run")
	require.NoError(t, err)
	assert.Equal(t, []authz.Subject{user(5)}, none, "only the computed role that includes system permissions")
}

func TestHoldersOfRefusesWhatItCannotAnswer(t *testing.T) {
	w := newSmallWorld(t)

	_, err := w.Engine.HoldersOf(context.Background(), "crm.lead:fly")
	require.ErrorIs(t, err, authz.ErrUnknownPermission)

	engine, err := authz.NewEngine(authz.Config{
		Catalogue: smallCatalogue(t), Hierarchy: authztest.Hierarchy(), Roles: smallRoles(), Store: readOnly{w.Store},
		Resolver: w.Places, Features: w.Features,
	})
	require.NoError(t, err)

	_, err = engine.HoldersOf(context.Background(), "crm.lead:view")
	require.ErrorContains(t, err, "HoldersOf needs a store")
}

// readOnly is a store that only reads, as an engine may be given.
type readOnly struct{ s authz.Reader }

func (r readOnly) BindingsFor(ctx context.Context, s authz.Subject) ([]authz.Binding, error) {
	return r.s.BindingsFor(ctx, s)
}

func (r readOnly) Role(ctx context.Context, id int) (authz.Role, error) { return r.s.Role(ctx, id) }
