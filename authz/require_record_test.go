//nolint:exhaustruct,exhaustruct_v5,goconst,lll,varnamelen // tests in the style of the package's others
package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
)

var (
	errLeadNotFound = errors.New("lead not found")
	errStoreAway    = errors.New("the store is away")
)

// scripted answers RequireOn per permission and records the order it was asked in.
type scripted struct {
	answers map[string]error
	asked   []string
}

func (s *scripted) Require(context.Context, string) error { return nil }

func (s *scripted) RequireOn(_ context.Context, permission string, _ authz.Resource) error {
	s.asked = append(s.asked, permission)

	return s.answers[permission]
}

func (s *scripted) Access(context.Context, string) (authz.AccessSet, error) {
	return authz.AccessSet{}, nil
}

func TestRequireRecord(t *testing.T) {
	t.Parallel()

	forbidden := authz.ErrForbidden

	tests := []struct {
		name      string
		answers   map[string]error
		action    string
		want      error
		wantAsked []string
	}{
		{"both pass", nil, "update", nil, []string{"view", "update"}},
		{"a refused view is not found, and the action is not asked", map[string]error{"view": forbidden}, "update", errLeadNotFound, []string{"view"}},
		{"a refused action after a passed view stays forbidden", map[string]error{"update": forbidden}, "update", authz.ErrForbidden, []string{"view", "update"}},
		{"the action being the view checks once", nil, "view", nil, []string{"view"}},
		{"the view as the action, refused, is not found", map[string]error{"view": forbidden}, "view", errLeadNotFound, []string{"view"}},
		{"another error of the view passes through", map[string]error{"view": errStoreAway}, "update", errStoreAway, []string{"view"}},
		{"another error of the action passes through", map[string]error{"update": errStoreAway}, "update", errStoreAway, []string{"view", "update"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			az := &scripted{answers: tt.answers}
			err := authz.RequireRecord(t.Context(), az, authz.Resource{}, "view", tt.action, errLeadNotFound)

			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.want)
			}

			assert.Equal(t, tt.wantAsked, az.asked)
		})
	}
}

// Over the real engine: a seller sees only their own leads, so a lead of another is not found, never forbidden.
func TestRequireRecordOverTheEngine(t *testing.T) {
	t.Parallel()

	const me, other = 5, 6

	w := newSmallWorld(t)
	w.Bind(user(me), "seller", authztest.Dealer(1))

	ctx := w.As(authz.User(me, 0))
	place := authztest.Location(10)
	mine, theirs, shared := authz.Resource{Scope: place, Owner: me}, authz.Resource{Scope: place, Owner: other}, authz.Resource{Scope: place, Owner: other, CoOwner: me}

	require.NoError(t, authz.RequireRecord(ctx, w.Engine, mine, "crm.lead:view", "crm.lead:update", errLeadNotFound))
	require.NoError(t, authz.RequireRecord(ctx, w.Engine, shared, "crm.lead:view", "crm.lead:update", errLeadNotFound))
	require.ErrorIs(t, authz.RequireRecord(ctx, w.Engine, theirs, "crm.lead:view", "crm.lead:update", errLeadNotFound), errLeadNotFound)
	require.ErrorIs(t, authz.RequireRecord(ctx, w.Engine, mine, "crm.lead:view", "crm.lead:assign", errLeadNotFound), authz.ErrForbidden)
}
