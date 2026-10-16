package identity_test

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
)

// ownLog is a change log of the application's own: it reads what it likes, and writes nothing.
type ownLog struct{ writes int }

func (l *ownLog) Record(context.Context, account.Change) error { l.writes++; return nil }

func (*ownLog) Changes(_ context.Context, q account.ChangeQuery) ([]account.ChangeEntry, int, error) {
	return []account.ChangeEntry{{ID: 1, Change: account.Change{AccountID: q.AccountID, Action: account.ChangeActivated}, Ext: "mine"}}, 1, nil
}

func (*ownLog) ChangeCounts(context.Context, int) (map[account.ChangeAction]int, error) {
	return map[account.ChangeAction]int{account.ChangeActivated: 1}, nil
}

// shop is a UserExtension with one number per person.
type shop struct{ dealer map[int]int }

func (s shop) Ext(_ context.Context, ids []int) (map[int]any, error) {
	out := map[int]any{}
	for _, id := range ids {
		out[id] = map[string]int{"dealer_id": s.dealer[id]}
	}

	return out, nil
}

func (s shop) Create(_ context.Context, id int, ext json.RawMessage, _ int) (any, error) {
	var in struct{ DealerID int }
	_ = json.Unmarshal(ext, &in)
	s.dealer[id] = in.DealerID

	return "created", nil
}

func (shop) Update(context.Context, int, json.RawMessage, int) error { return nil }

func (shop) Deactivate(context.Context, account.Account, json.RawMessage, int) (any, error) {
	return "moved", nil
}

// WithUserReach, WithUserExtension, WithChangeLog and WithIdleAfter are wired by Install into the routes.
func TestTheUserAdminHooksAreWiredByInstall(t *testing.T) {
	app := newApp(t)
	log := &ownLog{}
	ext := shop{dealer: map[int]int{}}

	users := identity.Install(app, identity.WithoutMail(),
		identity.WithUserReach(func(_ context.Context, permission string, id int) (bool, error) { return id != 3, nil }),
		identity.WithUserExtension(ext),
		identity.WithChangeLog(log),
		identity.WithIdleAfter(24*time.Hour),
	)

	for _, login := range []string{"ana", "boris", "cvita"} {
		_, err := users.Admin().Create(t.Context(), account.CreateAccount{
			Login: login, Name: login, Email: login + "@example.test", Locale: "en", Password: "a long password", Ext: json.RawMessage(`{"DealerID":4}`),
		})
		require.NoError(t, err)
	}

	assert.Equal(t, 3, log.writes, "the creations went to the application's change log")

	c := newClient(t, app)
	login(t, c, "ana", "a long password")

	assert.Equal(t, nethttp.StatusNotFound, c.do(nethttp.MethodGet, "/v1/iam/users/3", nil).Code, "cvita is out of reach")

	shown := c.do(nethttp.MethodGet, "/v1/iam/users/2", nil)
	require.Equal(t, nethttp.StatusOK, shown.Code, shown.Body.String())
	assert.Contains(t, shown.Body.String(), `"ext":{"dealer_id":4}`)

	changes := c.do(nethttp.MethodGet, "/v1/iam/users/2/changes", nil)
	require.Equal(t, nethttp.StatusOK, changes.Code, changes.Body.String())
	assert.Contains(t, changes.Body.String(), `"ext":"mine"`)

	idle := c.do(nethttp.MethodGet, "/v1/iam/users?view=all&last_sign_in=never", nil)
	require.Equal(t, nethttp.StatusOK, idle.Code, idle.Body.String())
	assert.Contains(t, idle.Body.String(), `"login":"boris"`)
	assert.NotContains(t, idle.Body.String(), `"login":"ana"`, "ana signed in")

	dec := c.do(nethttp.MethodPost, "/v1/iam/users/2/deactivate", map[string]any{"details": map[string]int{"leads": 1}})
	require.Equal(t, nethttp.StatusOK, dec.Code, dec.Body.String())
	assert.Contains(t, dec.Body.String(), `"ext":"moved"`)
}
