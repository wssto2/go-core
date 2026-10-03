package http_test

import (
	nethttp "net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz/authztest"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/identitytest"
)

// as signs in as ana and returns a request modifier that carries her token.
func (h *harness) as(login, password string) func(*nethttp.Request) {
	h.t.Helper()

	r := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": login, "password": password})
	require.Equal(h.t, nethttp.StatusOK, r.Code, r.Body.String())

	token := r.cookie("access_token").Value

	return func(req *nethttp.Request) { req.Header.Set("Authorization", "Bearer "+token) }
}

func data(t *testing.T, r reply) map[string]any {
	t.Helper()

	d, ok := r.json()["data"].(map[string]any)
	require.True(t, ok, r.Body.String())

	return d
}

// The permissions are fixed: viewing reads, managing writes, and the profile needs neither.
func TestUsersRoutesNeedTheirPermissions(t *testing.T) {
	deny := authztest.DenyAll().Allow(identityhttp.ViewUsers)
	h := newHarnessFull(t, "", deny, nil)
	ana := h.as("ana", "secret")

	for method, path := range map[string]string{nethttp.MethodGet: "/v1/iam/users"} {
		assert.Equal(t, nethttp.StatusOK, h.do(method, path, nil, ana).Code, "view reads")
	}

	for _, c := range []struct{ method, path string }{
		{nethttp.MethodPost, "/v1/iam/users"},
		{nethttp.MethodPut, "/v1/iam/users/2"},
		{nethttp.MethodPut, "/v1/iam/users/2/password"},
		{nethttp.MethodPost, "/v1/iam/users/2/deactivate"},
		{nethttp.MethodPost, "/v1/iam/users/2/activate"},
		{nethttp.MethodPost, "/v1/iam/users/2/unlock"},
		{nethttp.MethodDelete, "/v1/iam/users/2/sessions"},
		{nethttp.MethodDelete, "/v1/iam/users/2/sessions/1"},
	} {
		assert.Equal(t, nethttp.StatusForbidden, h.do(c.method, c.path, map[string]any{}, ana).Code, c.method+" "+c.path+" needs iam.user:manage")
	}

	none := newHarnessFull(t, "", authztest.DenyAll(), nil)
	token := none.as("ana", "secret")

	assert.Equal(t, nethttp.StatusForbidden, none.do(nethttp.MethodGet, "/v1/iam/users", nil, token).Code)
	assert.Equal(t, nethttp.StatusOK, none.do(nethttp.MethodGet, "/v1/iam/profile", nil, token).Code, "a person's own profile needs no permission")
	assert.Equal(t, nethttp.StatusUnauthorized, none.do(nethttp.MethodGet, "/v1/iam/profile", nil).Code)
	assert.Equal(t, nethttp.StatusUnauthorized, none.do(nethttp.MethodGet, "/v1/iam/users", nil).Code)
}

// The list is go-core's datatable page.
func TestTheListIsADatatablePage(t *testing.T) {
	h := newHarness(t, nil)
	ana := h.as("ana", "secret")

	r := h.do(nethttp.MethodGet, "/v1/iam/users?view=all&order_col=login&order_dir=desc&per_page=2&page=1", nil, ana)
	require.Equal(t, nethttp.StatusOK, r.Code, r.Body.String())

	page := r.json()
	body, _ := page["data"].(map[string]any)

	if body == nil { // the page may be the envelope's data or the envelope itself
		body = page
	}

	assert.EqualValues(t, 3, body["total"])
	assert.EqualValues(t, 2, body["per_page"])
	assert.EqualValues(t, 1, body["current_page"])
	assert.EqualValues(t, 2, body["last_page"])
	assert.EqualValues(t, 1, body["from"])
	assert.EqualValues(t, 2, body["to"])

	rows, _ := body["data"].([]any)
	require.Len(t, rows, 2)

	first, _ := rows[0].(map[string]any)
	assert.Equal(t, "ines", first["login"])
	assert.Equal(t, "inactive", first["status"])
	assert.Nil(t, first["last_sign_in"])
	assert.Nil(t, first["locked_until"])
	assert.NotContains(t, r.Body.String(), "password")

	second, _ := rows[1].(map[string]any)
	assert.Equal(t, "boris", second["login"])
	assert.Equal(t, "active", second["status"])

	// the default view: active, not locked
	def := h.do(nethttp.MethodGet, "/v1/iam/users", nil, ana)
	require.Equal(t, nethttp.StatusOK, def.Code)

	bad := h.do(nethttp.MethodGet, "/v1/iam/users?view=everyone", nil, ana)
	assert.Equal(t, nethttp.StatusUnprocessableEntity, bad.Code)
	assert.Equal(t, "identity.list.view_invalid", bad.json()["code"])

	badDir := h.do(nethttp.MethodGet, "/v1/iam/users?order_dir=sideways", nil, ana)
	assert.Equal(t, nethttp.StatusUnprocessableEntity, badDir.Code)
}

func TestCreateShowUpdateAndDeactivateOverHTTP(t *testing.T) {
	h := newHarness(t, nil)
	ana := h.as("ana", "secret")

	created := h.do(nethttp.MethodPost, "/v1/iam/users", map[string]string{
		"login": "Dora", "name": "Dora Horvat", "email": "dora@example.test", "phone": "", "locale": "hr", "password": "a long password",
	}, ana)
	require.Equal(t, nethttp.StatusOK, created.Code, created.Body.String())

	user := data(t, created)
	assert.Equal(t, "dora", user["login"])
	assert.Equal(t, "active", user["status"])
	assert.NotContains(t, created.Body.String(), "password")

	id := int(user["id"].(float64))

	dup := h.do(nethttp.MethodPost, "/v1/iam/users", map[string]string{
		"login": "dora", "name": "x", "email": "x@example.test", "locale": "hr", "password": "a long password",
	}, ana)
	assert.Equal(t, nethttp.StatusConflict, dup.Code)
	assert.Equal(t, "identity.login.taken", dup.json()["code"])

	weak := h.do(nethttp.MethodPost, "/v1/iam/users", map[string]string{
		"login": "eva", "name": "Eva", "email": "eva@example.test", "locale": "hr", "password": "short",
	}, ana)
	assert.Equal(t, nethttp.StatusUnprocessableEntity, weak.Code)
	assert.Equal(t, "identity.password.weak", weak.json()["code"])

	empty := h.do(nethttp.MethodPost, "/v1/iam/users", map[string]string{"login": ""}, ana)
	assert.Equal(t, nethttp.StatusUnprocessableEntity, empty.Code, "the input's own validation")

	updated := h.do(nethttp.MethodPut, "/v1/iam/users/"+itoa(id), map[string]string{
		"login": "dora", "name": "Dora H.", "email": "dora@example.test", "phone": "123", "locale": "en",
	}, ana)
	require.Equal(t, nethttp.StatusOK, updated.Code, updated.Body.String())
	assert.Equal(t, "Dora H.", data(t, updated)["name"])

	shown := h.do(nethttp.MethodGet, "/v1/iam/users/"+itoa(id), nil, ana)
	assert.Equal(t, "123", data(t, shown)["phone"])

	changes := h.do(nethttp.MethodGet, "/v1/iam/users/"+itoa(id)+"/changes", nil, ana)
	require.Equal(t, nethttp.StatusOK, changes.Code, changes.Body.String())

	rows := changesRows(t, changes)
	require.Len(t, rows, 2)
	assert.Equal(t, "updated", rows[0]["action"])
	assert.Equal(t, "created", rows[1]["action"])
	assert.EqualValues(t, 1, rows[0]["actor_id"], "ana did it")

	assert.Equal(t, nethttp.StatusNoContent, h.do(nethttp.MethodPost, "/v1/iam/users/"+itoa(id)+"/deactivate", nil, ana).Code)

	again := h.do(nethttp.MethodPost, "/v1/iam/users/"+itoa(id)+"/deactivate", nil, ana)
	assert.Equal(t, nethttp.StatusConflict, again.Code)
	assert.Equal(t, "identity.account.already_inactive", again.json()["code"])

	self := h.do(nethttp.MethodPost, "/v1/iam/users/1/deactivate", nil, ana)
	assert.Equal(t, nethttp.StatusBadRequest, self.Code)
	assert.Equal(t, "identity.account.self_deactivation", self.json()["code"])

	assert.Equal(t, nethttp.StatusNoContent, h.do(nethttp.MethodPost, "/v1/iam/users/"+itoa(id)+"/activate", nil, ana).Code)

	missing := h.do(nethttp.MethodGet, "/v1/iam/users/999", nil, ana)
	assert.Equal(t, nethttp.StatusNotFound, missing.Code)
}

func itoa(n int) string { return strconv.Itoa(n) }

func changesRows(t *testing.T, r reply) []map[string]any {
	t.Helper()

	page := r.json()

	body, _ := page["data"].(map[string]any)
	if body == nil {
		body = page
	}

	raw, _ := body["data"].([]any)
	out := make([]map[string]any, len(raw))

	for i, row := range raw {
		out[i], _ = row.(map[string]any)
	}

	return out
}

// IAM-USER-002 items 6 and 7 on the wire.
func TestUnlockAndANewPasswordOverHTTP(t *testing.T) {
	h := newHarness(t, nil)
	ana := h.as("ana", "secret")

	for range 5 {
		h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "boris", "password": "nope"})
	}

	locked := h.do(nethttp.MethodGet, "/v1/iam/users/2", nil, ana)
	assert.Equal(t, "locked", data(t, locked)["status"])

	unlock := h.do(nethttp.MethodPost, "/v1/iam/users/2/unlock", nil, ana)
	require.Equal(t, nethttp.StatusOK, unlock.Code, unlock.Body.String())
	assert.Equal(t, true, data(t, unlock)["unlocked"])
	assert.Equal(t, false, data(t, h.do(nethttp.MethodPost, "/v1/iam/users/2/unlock", nil, ana))["unlocked"])

	h.kit.Clock.Advance(61 * time.Second) // a new minute: the attempts of the first are not counted against the second

	for range 5 {
		h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "boris", "password": "nope"})
	}

	set := h.do(nethttp.MethodPut, "/v1/iam/users/2/password", map[string]string{"password": "a brand new one"}, ana)
	assert.Equal(t, nethttp.StatusNoContent, set.Code, set.Body.String())

	in := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "boris", "password": "a brand new one"})
	assert.Equal(t, nethttp.StatusOK, in.Code, "the lock is lifted")

	signins := h.do(nethttp.MethodGet, "/v1/iam/users/2/signins?per_page=3", nil, ana)
	require.Equal(t, nethttp.StatusOK, signins.Code, signins.Body.String())

	rows := changesRows(t, signins)
	require.Len(t, rows, 3)
	assert.Equal(t, "signed_in", rows[0]["event"])
}

func TestSessionsOfAPersonOverHTTP(t *testing.T) {
	h := newHarness(t, nil)
	ana := h.as("ana", "secret")
	h.as("boris", "hunter2")

	list := h.do(nethttp.MethodGet, "/v1/iam/users/2/sessions", nil, ana)
	require.Equal(t, nethttp.StatusOK, list.Code, list.Body.String())

	sessions, _ := data(t, list)["sessions"].([]any)
	require.Len(t, sessions, 1)

	first, _ := sessions[0].(map[string]any)
	assert.Equal(t, false, first["current"])

	revoke := h.do(nethttp.MethodDelete, "/v1/iam/users/2/sessions/"+itoa(int(first["id"].(float64))), nil, ana)
	assert.Equal(t, nethttp.StatusNoContent, revoke.Code, revoke.Body.String())

	h.as("boris", "hunter2")

	all := h.do(nethttp.MethodDelete, "/v1/iam/users/2/sessions", nil, ana)
	assert.Equal(t, nethttp.StatusNoContent, all.Code)

	list = h.do(nethttp.MethodGet, "/v1/iam/users/2/sessions", nil, ana)
	empty, _ := data(t, list)["sessions"].([]any)
	assert.Empty(t, empty)
}

func TestProfileOverHTTP(t *testing.T) {
	h := newHarness(t, nil)
	ana := h.as("ana", "secret")

	show := h.do(nethttp.MethodGet, "/v1/iam/profile", nil, ana)
	require.Equal(t, nethttp.StatusOK, show.Code, show.Body.String())
	assert.Equal(t, "ana@example.test", data(t, show)["email"])
	assert.Nil(t, data(t, show)["pending_email"])

	upd := h.do(nethttp.MethodPut, "/v1/iam/profile", map[string]string{"name": "Ana A.", "phone": "099"}, ana)
	require.Equal(t, nethttp.StatusOK, upd.Code, upd.Body.String())
	assert.Equal(t, "Ana A.", data(t, upd)["name"])

	// the wrong current password is a field error, not a 401
	wrong := h.do(nethttp.MethodPut, "/v1/iam/profile/password", map[string]string{
		"current_password": "nope", "new_password": "a better one", "new_password_confirmation": "a better one",
	}, ana)
	assert.Equal(t, nethttp.StatusUnprocessableEntity, wrong.Code)
	assert.Equal(t, "identity.password.wrong", wrong.json()["code"])
	assert.Contains(t, wrong.Body.String(), "current_password")

	// request, resend and confirm an address change
	req := h.do(nethttp.MethodPost, "/v1/iam/profile/email", map[string]string{"email": "new@example.test", "current_password": "secret"}, ana)
	require.Equal(t, nethttp.StatusOK, req.Code, req.Body.String())
	assert.Equal(t, "new@example.test", data(t, req)["email"])
	assert.NotContains(t, req.Body.String(), h.kit.Mailbox.Last().Code, "the code travels by mail only")

	pending := h.do(nethttp.MethodGet, "/v1/iam/profile", nil, ana)
	assert.Equal(t, "new@example.test", data(t, pending)["pending_email"].(map[string]any)["email"])

	h.kit.Clock.Advance(61 * time.Second)
	resend := h.do(nethttp.MethodPost, "/v1/iam/profile/email/resend", nil, ana)
	assert.Equal(t, nethttp.StatusOK, resend.Code, resend.Body.String())

	badCode := h.do(nethttp.MethodPost, "/v1/iam/profile/email/confirm", map[string]string{"code": "12"}, ana)
	assert.Equal(t, nethttp.StatusUnprocessableEntity, badCode.Code)

	confirm := h.do(nethttp.MethodPost, "/v1/iam/profile/email/confirm", map[string]string{"code": h.kit.Mailbox.Last().Code}, ana)
	require.Equal(t, nethttp.StatusOK, confirm.Code, confirm.Body.String())
	assert.Equal(t, "new@example.test", data(t, confirm)["email"])

	cancel := h.do(nethttp.MethodDelete, "/v1/iam/profile/email", nil, ana)
	assert.Equal(t, nethttp.StatusNoContent, cancel.Code)

	signins := h.do(nethttp.MethodGet, "/v1/iam/profile/signins", nil, ana)
	assert.Equal(t, nethttp.StatusOK, signins.Code)

	// a good password change keeps this session and ends the others
	other := h.as("ana", "secret")
	ok := h.do(nethttp.MethodPut, "/v1/iam/profile/password", map[string]string{
		"current_password": "secret", "new_password": "a better one", "new_password_confirmation": "a better one",
	}, ana)
	assert.Equal(t, nethttp.StatusNoContent, ok.Code, ok.Body.String())

	assert.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodGet, "/v1/iam/profile", nil, ana).Code, "the session that made the change stays")
	assert.Equal(t, nethttp.StatusUnauthorized, h.do(nethttp.MethodGet, "/v1/iam/profile", nil, other).Code, "the others are signed out")
}

func TestOwnSessionsMarkTheCurrentOne(t *testing.T) {
	h := newHarness(t, nil)
	ana := h.as("ana", "secret")
	h.as("ana", "secret")

	list := h.do(nethttp.MethodGet, "/v1/iam/profile/sessions", nil, ana)
	require.Equal(t, nethttp.StatusOK, list.Code, list.Body.String())

	sessions, _ := data(t, list)["sessions"].([]any)
	require.Len(t, sessions, 2)

	var current, other int

	for _, s := range sessions {
		m, _ := s.(map[string]any)
		if m["current"] == true {
			current = int(m["id"].(float64))
		} else {
			other = int(m["id"].(float64))
		}
	}

	self := h.do(nethttp.MethodDelete, "/v1/iam/profile/sessions/"+itoa(current), nil, ana)
	assert.Equal(t, nethttp.StatusBadRequest, self.Code, "ending the current one is signing out")
	assert.Equal(t, "identity.session.current", self.json()["code"])

	assert.Equal(t, nethttp.StatusNoContent, h.do(nethttp.MethodDelete, "/v1/iam/profile/sessions/"+itoa(other), nil, ana).Code)
}

func TestWithoutMailTheAddressChangeIsRefusedOverHTTP(t *testing.T) {
	h := newHarness(t, nil, identitytest.WithoutMail())
	ana := h.as("ana", "secret")

	r := h.do(nethttp.MethodPost, "/v1/iam/profile/email", map[string]string{"email": "new@example.test", "current_password": "secret"}, ana)
	assert.Equal(t, nethttp.StatusBadRequest, r.Code)
	assert.Equal(t, "identity.email.disabled", r.json()["code"])
}
