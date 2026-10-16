//nolint:exhaustruct,exhaustruct_v5,goconst,lll,misspell,paralleltest,varnamelen // tests in the style of the package's others
package http_test

import (
	"context"
	"fmt"
	nethttp "net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/identity/account"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/route"
)

// reachCase is one users route that names a person, as it is called.
type reachCase struct {
	route  route.Declared
	method string
	path   string // %d is the person
	body   any
}

// reachCases are every route that names a person; TestEveryUserRouteAsksTheReach is the guard that a new one is not forgotten.
func reachCases() []reachCase {
	return []reachCase{
		{identityhttp.ShowUser, nethttp.MethodGet, "/v1/iam/users/%d", nil},
		{identityhttp.UpdateUser, nethttp.MethodPut, "/v1/iam/users/%d", map[string]any{"login": "x", "name": "X", "email": "x@example.test", "locale": "hr"}},
		{identityhttp.SetUserPassword, nethttp.MethodPut, "/v1/iam/users/%d/password", map[string]any{"password": "brand-new-password"}},
		{identityhttp.DeactivateUser, nethttp.MethodPost, "/v1/iam/users/%d/deactivate", map[string]any{}},
		{identityhttp.ActivateUser, nethttp.MethodPost, "/v1/iam/users/%d/activate", map[string]any{}},
		{identityhttp.UnlockUser, nethttp.MethodPost, "/v1/iam/users/%d/unlock", map[string]any{}},
		{identityhttp.UserSignIns, nethttp.MethodGet, "/v1/iam/users/%d/signins", nil},
		{identityhttp.UserChanges, nethttp.MethodGet, "/v1/iam/users/%d/changes", nil},
		{identityhttp.UserActivity, nethttp.MethodGet, "/v1/iam/users/%d/activity", nil},
		{identityhttp.UserSessions, nethttp.MethodGet, "/v1/iam/users/%d/sessions", nil},
		{identityhttp.RevokeUserSession, nethttp.MethodDelete, "/v1/iam/users/%d/sessions/1", nil},
		{identityhttp.RevokeUserSessions, nethttp.MethodDelete, "/v1/iam/users/%d/sessions", nil},
	}
}

func at(path string, id int) string { return fmt.Sprintf(path, id) }

// asked records what the reach function was asked.
type asked struct {
	mu   sync.Mutex
	logs []string
}

func (a *asked) add(permission string, id int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.logs = append(a.logs, permission+" "+itoa(id))
}

func (a *asked) take() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := a.logs
	a.logs = nil

	return out
}

// Every route that names a person is behind the reach: a person out of reach is refused like one who
// does not exist, the reach is asked with the permission of the route, and nothing is changed.
func TestEveryUserRouteAsksTheReach(t *testing.T) {
	var log asked

	h := newHarness(t, func(c *identityhttp.Config) {
		c.Reach = func(_ context.Context, permission string, id int) (bool, error) {
			log.add(permission, id)

			return id != 2, nil // boris is out of reach
		}
	})
	ana := h.as("ana", "secret")

	covered := map[string]bool{}

	for _, tc := range reachCases() {
		covered[tc.route.Spec().Name] = true
		name := tc.method + " " + tc.path

		log.take()

		out := h.do(tc.method, at(tc.path, 2), tc.body, ana)
		assert.Equal(t, nethttp.StatusNotFound, out.Code, name+": out of reach is refused: "+out.Body.String())
		assert.Equal(t, "identity.account.not_found", out.json()["code"], name)
		assert.Equal(t, []string{tc.route.Spec().Permission + " 2"}, log.take(), name+": asked once, with the route's permission")

		// the answer is the one for a person who does not exist
		gone := h.do(tc.method, at(tc.path, 99), tc.body, ana)
		assert.Equal(t, nethttp.StatusNotFound, gone.Code, name)
		assert.JSONEq(t, gone.Body.String(), out.Body.String(), name+": out of reach looks like not found")

		// a person in reach is served (the route's own answer, whatever it is, is not the refusal)
		in := h.do(tc.method, at(tc.path, 3), tc.body, ana)
		assert.NotEqual(t, "identity.account.not_found", in.json()["code"], name+": in reach: "+in.Body.String())
	}

	// no route that names a person is left out of this test
	for _, spec := range identityhttp.Routes.Specs() {
		if spec.Permission != identityhttp.ViewUsers && spec.Permission != identityhttp.ManageUsers && spec.Permission != identityhttp.ViewActivity {
			continue
		}

		if spec.Path == "/v1/iam/users" { // the list and create name no person
			continue
		}

		assert.True(t, covered[spec.Name], "%s names a person but TestEveryUserRouteAsksTheReach does not cover it", spec.Name)
	}
}

// What is out of reach is not changed: nothing was written before the refusal.
func TestAnOutOfReachPersonIsLeftAlone(t *testing.T) {
	h := newHarness(t, func(c *identityhttp.Config) {
		c.Reach = func(_ context.Context, _ string, id int) (bool, error) { return id != 2, nil }
	})
	ana := h.as("ana", "secret")

	before, err := h.kit.Accounts.Find(t.Context(), 2)
	require.NoError(t, err)

	for _, tc := range reachCases() {
		h.do(tc.method, at(tc.path, 2), tc.body, ana)
	}

	after, err := h.kit.Accounts.Find(t.Context(), 2)
	require.NoError(t, err)

	assert.Equal(t, before, after, "name, login, password hash and status are as they were")

	rows, total, err := h.kit.Changes.Changes(t.Context(), account.ChangeQuery{AccountID: 2, Limit: 10})
	require.NoError(t, err)
	assert.Zero(t, total, "no change is on its history: %v", rows)

	for _, e := range h.kit.SignIns.All() {
		assert.NotEqual(t, 2, e.AccountID, "nothing is on its sign-in history (an unlock, a sign-out): %v", e)
	}
}

// An error from the reach is the answer, and the route does nothing.
func TestAReachErrorIsTheAnswer(t *testing.T) {
	h := newHarness(t, func(c *identityhttp.Config) {
		c.Reach = func(context.Context, string, int) (bool, error) { return false, account.ErrNotFound }
	})
	ana := h.as("ana", "secret")

	out := h.do(nethttp.MethodGet, "/v1/iam/users/2", nil, ana)
	assert.Equal(t, nethttp.StatusInternalServerError, out.Code, "an error that is not an application error answers 500")
}

// Without the option the routes behave as they always did: the list, create and routes that name
// a person all work for whoever holds the permission.
func TestWithoutReachEveryPersonIsServed(t *testing.T) {
	h := newHarness(t, nil)
	ana := h.as("ana", "secret")

	for _, tc := range reachCases() {
		out := h.do(tc.method, at(tc.path, 2), tc.body, ana)
		assert.NotEqual(t, "identity.account.not_found", out.json()["code"], tc.method+" "+tc.path+": "+out.Body.String())
	}

	assert.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodGet, "/v1/iam/users/2", nil, ana).Code)
}

// The list and create name no person, so the reach is not asked for them.
func TestTheListAndCreateAreNotAskedOfTheReach(t *testing.T) {
	var log asked

	h := newHarness(t, func(c *identityhttp.Config) {
		c.Reach = func(_ context.Context, permission string, id int) (bool, error) {
			log.add(permission, id)

			return false, nil
		}
	})
	ana := h.as("ana", "secret")

	assert.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodGet, "/v1/iam/users", nil, ana).Code)
	assert.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodPost, "/v1/iam/users", map[string]any{
		"login": "novi", "name": "Novi", "email": "novi@example.test", "locale": "hr", "password": "long-enough-password",
	}, ana).Code)
	assert.Empty(t, log.take())
}
