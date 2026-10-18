//nolint:exhaustruct,exhaustruct_v5,goconst,lll,paralleltest,varnamelen // tests in the style of the package's others
package identity_test

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/preferences"
	"github.com/wssto2/go-core/identity/serviceaccount"
)

const preferencesPath = "/v1/auth/preferences"

// mapStore is an application's own store of preferences.
type mapStore struct{ rows map[int]json.RawMessage }

func (m *mapStore) Get(_ context.Context, id int) (json.RawMessage, error) { return m.rows[id], nil }

func (m *mapStore) Set(_ context.Context, id int, p json.RawMessage) error {
	m.rows[id] = p

	return nil
}

// WithPreferences on identity's own table: the route, the merge, and the payload, for a person.
func TestPreferencesLiveInIdentitysTable(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{}, identity.WithPreferences(identity.Preferences{}))

	require.True(t, r.db.Migrator().HasTable("account_preferences"))

	got := r.web.do(nethttp.MethodPut, preferencesPath, map[string]any{"preferences": map[string]any{"dark_mode": true, "onboarding": map[string]any{"welcome_done": true}}})
	require.Equal(t, nethttp.StatusOK, got.Code, got.Body.String())
	assert.JSONEq(t, `{"dark_mode":true,"onboarding":{"welcome_done":true}}`, string(data[struct {
		Preferences json.RawMessage `json:"preferences"`
	}](t, got).Preferences))

	got = r.web.do(nethttp.MethodPut, preferencesPath, map[string]any{"preferences": map[string]any{"onboarding": map[string]any{"tips": []string{"a"}}}})
	assert.JSONEq(t, `{"dark_mode":true,"onboarding":{"welcome_done":true,"tips":["a"]}}`, string(data[struct {
		Preferences json.RawMessage `json:"preferences"`
	}](t, got).Preferences))

	me := r.web.do(nethttp.MethodGet, "/v1/auth/me", nil)
	assert.JSONEq(t, `{"dark_mode":true,"onboarding":{"welcome_done":true,"tips":["a"]}}`, string(data[struct {
		Preferences json.RawMessage `json:"preferences"`
	}](t, me).Preferences))

	var rows int64
	require.NoError(t, r.db.Table("account_preferences").Count(&rows).Error)
	assert.EqualValues(t, 1, rows, "one row per person, updated in place")
}

// A person's own record: a key is refused (403), and so is somebody who is not signed in (401).
func TestPreferencesRefuseAServiceAccountAndAStranger(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{}, identity.WithPreferences(identity.Preferences{}))
	_, secret := r.account("DMS export")

	rec := r.withKey(nethttp.MethodPut, preferencesPath, secret, map[string]any{"preferences": map[string]any{"dark_mode": true}})
	assert.Equal(t, nethttp.StatusForbidden, rec.Code, rec.Body.String())

	stranger := newClient(t, r.app)
	assert.Equal(t, nethttp.StatusUnauthorized, stranger.do(nethttp.MethodPut, preferencesPath, map[string]any{"preferences": map[string]any{}}).Code)

	var rows int64
	require.NoError(t, r.db.Table("account_preferences").Count(&rows).Error)
	assert.Zero(t, rows, "nothing was written")
}

// An application's own store brings its own table: identity makes none, and the application's rules apply.
func TestPreferencesOverTheApplicationsStoreMakeNoTable(t *testing.T) {
	store := &mapStore{rows: map[int]json.RawMessage{}}

	r := newRobots(t, serviceaccount.Config{}, identity.WithPreferences(identity.Preferences{
		Store: store,
		Validate: func(merged json.RawMessage) error {
			if len(merged) > 40 {
				return preferences.Invalid("preferences.big", "max")
			}

			return nil
		},
	}))

	assert.False(t, r.db.Migrator().HasTable("account_preferences"))

	ok := r.web.do(nethttp.MethodPut, preferencesPath, map[string]any{"preferences": map[string]any{"dark_mode": true}})
	require.Equal(t, nethttp.StatusOK, ok.Code, ok.Body.String())
	assert.JSONEq(t, `{"dark_mode":true}`, string(store.rows[1]), "the webmaster is account 1")

	bad := r.web.do(nethttp.MethodPut, preferencesPath, map[string]any{"preferences": map[string]any{"a": "0123456789012345678901234567890123456789"}})
	require.Equal(t, nethttp.StatusUnprocessableEntity, bad.Code, bad.Body.String())
	assert.Contains(t, bad.Body.String(), "preferences.big")
}

// Without the option nothing changes: no table, no route, no field in the payload.
func TestWithoutPreferencesThereIsNothing(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{})

	assert.False(t, r.db.Migrator().HasTable("account_preferences"))
	assert.Equal(t, nethttp.StatusNotFound, r.web.do(nethttp.MethodPut, preferencesPath, map[string]any{"preferences": map[string]any{"a": 1}}).Code)
	assert.NotContains(t, r.web.do(nethttp.MethodGet, "/v1/auth/me", nil).Body.String(), "preferences")
}

// The tables go where identity's other tables go.
func TestPreferencesFollowOn(t *testing.T) {
	app := newApp(t, gocoretest.Databases("local", "shared"))
	identity.Install(app, identity.WithoutMail(), identity.On(Shared), identity.WithPreferences(identity.Preferences{}))
	require.NoError(t, app.Check())

	assert.True(t, app.Database(Shared).Migrator().HasTable("account_preferences"))
	assert.False(t, app.Database().Migrator().HasTable("account_preferences"))
}
