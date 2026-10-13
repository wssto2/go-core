package identity_test

import (
	"context"
	"fmt"
	nethttp "net/http"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/mail"
)

// facts hears only the two facts a host's token cache cares about.
type facts struct {
	account.DiscardNotices
	mu  sync.Mutex
	got []string
}

func (f *facts) add(s string) { f.mu.Lock(); f.got = append(f.got, s); f.mu.Unlock() }

func (f *facts) SessionsEnded(_ context.Context, e account.SessionsEnded) {
	f.add(fmt.Sprintf("ended %d %v %s by %d", e.AccountID, e.SessionIDs, e.Reason, e.ActorID))
}

func (f *facts) AccountChanged(_ context.Context, id int) { f.add("changed " + strconv.Itoa(id)) }

func (f *facts) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := f.got
	f.got = nil

	return out
}

// Each action that ends a session or changes an account tells the Notices once, with
// the ids that ended, and a refused action tells nothing.
func TestNoticesHearSessionsEndedAndAccountChangedOverHTTP(t *testing.T) {
	app := newApp(t)
	f := &facts{}
	sink := mail.NewSink()

	identity.Install(app, identity.WithMail(sink), identity.WithCodeSecret(secret), identity.WithNotices(f),
		identity.AllowImpersonation("iam.user:impersonate"))

	ana := identitytest.Account(1, "ana", "secret")
	ana.Email = "ana@old.example"
	seed(t, app.Database(), ana)
	seed(t, app.Database(), identitytest.Account(2, "boris", "hunter2"))

	ids := func(user int) []int {
		var out []int
		require.NoError(t, app.Database().Model(new(auth.Token)).Where("user_id = ? AND revoked = ?", user, false).Order("id").Pluck("id", &out).Error)

		return out
	}
	sid := func(n int) string { return strconv.Itoa(n) }

	c := newClient(t, app)
	login(t, c, "ana", "secret")
	first := ids(1)[0]
	other := openSession(t, app, 1) // a second session of ana, for others to end
	_ = other
	f.take()

	do := func(method, path string, body any, want int) {
		t.Helper()
		r := c.do(method, path, body)
		if want == nethttp.StatusOK && r.Code == nethttp.StatusNoContent { // an empty answer
			want = r.Code
		}
		require.Equal(t, want, r.Code, method+" "+path+" "+r.Body.String())
	}

	// refused actions tell nothing
	do(nethttp.MethodDelete, "/v1/iam/profile/sessions/9999", nil, nethttp.StatusNotFound)
	do(nethttp.MethodPut, "/v1/iam/profile/password", map[string]string{
		"current_password": "wrong", "new_password": "N3w-secret-pw!", "new_password_confirmation": "N3w-secret-pw!",
	}, nethttp.StatusUnprocessableEntity)
	do(nethttp.MethodPost, "/v1/auth/change-locale", map[string]string{"locale": "!!"}, nethttp.StatusBadRequest)
	assert.Empty(t, f.take(), "a failed action fires nothing")

	// locale and profile
	do(nethttp.MethodPost, "/v1/auth/change-locale", map[string]string{"locale": "hr"}, nethttp.StatusOK)
	assert.Equal(t, []string{"changed 1"}, f.take())

	do(nethttp.MethodPut, "/v1/iam/profile", map[string]string{"name": "Ana Anić", "phone": "123"}, nethttp.StatusOK)
	assert.Equal(t, []string{"changed 1"}, f.take())

	do(nethttp.MethodPut, "/v1/iam/profile", map[string]string{"name": "Ana Anić", "phone": "123"}, nethttp.StatusOK)
	assert.Empty(t, f.take(), "nothing differed, nothing changed")

	// e-mail confirm
	do(nethttp.MethodPost, "/v1/iam/profile/email", map[string]string{"email": "ana@new.example", "current_password": "secret"}, nethttp.StatusOK)
	code := regexp.MustCompile(`\b\d{6}\b`).FindString(sink.To("ana@new.example")[0].Text)
	assert.Empty(t, f.take(), "the request alone changes nothing")
	do(nethttp.MethodPost, "/v1/iam/profile/email/confirm", map[string]string{"code": code}, nethttp.StatusOK)
	assert.Equal(t, []string{"changed 1"}, f.take())

	// ending another session of one's own
	second := ids(1)[1]
	do(nethttp.MethodDelete, "/v1/iam/profile/sessions/"+sid(second), nil, nethttp.StatusOK)
	assert.Equal(t, []string{fmt.Sprintf("ended 1 [%d] revoke by 0", second)}, f.take())

	// refresh: the old access token is replaced
	do(nethttp.MethodPost, "/v1/auth/refresh", map[string]string{}, nethttp.StatusOK)
	assert.Equal(t, []string{fmt.Sprintf("ended 1 [%d] rotation by 0", first)}, f.take())

	// administrator on boris
	boris := openSession(t, app, 2)
	_ = boris
	b1 := ids(2)[0]
	do(nethttp.MethodDelete, "/v1/iam/users/2/sessions/"+sid(b1), nil, nethttp.StatusOK)
	assert.Equal(t, []string{fmt.Sprintf("ended 2 [%d] revoke by 1", b1)}, f.take())

	openSession(t, app, 2)
	openSession(t, app, 2)
	b2 := ids(2)
	do(nethttp.MethodDelete, "/v1/iam/users/2/sessions", nil, nethttp.StatusOK)
	assert.Equal(t, []string{fmt.Sprintf("ended 2 %v revoke by 1", b2)}, f.take())

	do(nethttp.MethodPut, "/v1/iam/users/2", map[string]string{"login": "boris", "name": "Boris B", "email": "boris@x.example", "locale": "en"}, nethttp.StatusOK)
	assert.Equal(t, []string{"changed 2"}, f.take())

	openSession(t, app, 2)
	b3 := ids(2)
	do(nethttp.MethodPut, "/v1/iam/users/2/password", map[string]string{"password": "N3w-secret-pw!"}, nethttp.StatusOK)
	assert.Equal(t, []string{fmt.Sprintf("ended 2 %v password_changed by 1", b3), "changed 2"}, f.take())

	openSession(t, app, 2)
	b4 := ids(2)
	do(nethttp.MethodPost, "/v1/iam/users/2/deactivate", nil, nethttp.StatusOK)
	assert.Equal(t, []string{fmt.Sprintf("ended 2 %v deactivated by 1", b4), "changed 2"}, f.take())

	do(nethttp.MethodPost, "/v1/iam/users/2/activate", nil, nethttp.StatusOK)
	assert.Equal(t, []string{"changed 2"}, f.take())

	// login as boris, return
	do(nethttp.MethodPost, "/v1/auth/login-as", map[string]int{"user_id": 2}, nethttp.StatusOK)
	f.take()
	asBoris := ids(2)[0]
	do(nethttp.MethodPost, "/v1/auth/login-as/return", nil, nethttp.StatusOK)
	assert.Equal(t, []string{fmt.Sprintf("ended 2 [%d] login_as_return by 0", asBoris)}, f.take())

	// own password change ends the others and keeps this one
	do(nethttp.MethodPost, "/v1/auth/login-as", map[string]int{"user_id": 2}, nethttp.StatusOK)
	f.take()
	c2 := newClient(t, app)
	login(t, c2, "boris", "N3w-secret-pw!")
	f.take()
	live := ids(2)
	r := c2.do(nethttp.MethodPut, "/v1/iam/profile/password", map[string]string{
		"current_password": "N3w-secret-pw!", "new_password": "An0ther-pw-99!", "new_password_confirmation": "An0ther-pw-99!",
	})
	require.Equal(t, nethttp.StatusNoContent, r.Code, r.Body.String())
	got := f.take()
	require.Len(t, got, 2)
	assert.Contains(t, got[0], "password_changed by 0")
	assert.Equal(t, "changed 2", got[1])
	assert.Greater(t, len(live), 1)

	// logout
	c3 := newClient(t, app)
	login(t, c3, "ana", "secret")
	f.take()
	mine := ids(1)
	r = c3.do(nethttp.MethodPost, "/v1/auth/logout", nil)
	require.Equal(t, nethttp.StatusNoContent, r.Code, r.Body.String())
	assert.Equal(t, []string{fmt.Sprintf("ended 1 [%d] logout by 0", mine[len(mine)-1])}, f.take())
}
