package http_test

import (
	"context"
	nethttp "net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/identity/account"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/identitytest"
)

const (
	usersPath     = "/v1/iam/users"
	profilePath   = "/v1/iam/profile"
	password      = "a long password"
	secret        = "secret"
	localeField   = "locale"
	loginField    = "login"
	nameField     = "name"
	phoneField    = "phone"
	passwordField = "password"
	emailField    = "email"
	currentPass   = "current_password"
	userIDField   = "user_id"
)

// narrow is a store like the legacy users table: login 15, name 50, e-mail 50, phone 20.
func narrow() account.Limits { return account.Limits{Login: 15, Name: 50, Email: 50, Phone: 20} }

func limited(t *testing.T) *harness {
	t.Helper()

	limits := narrow()
	rules := account.Config{
		TokenTTL: 0, Lock: account.Lock{After: 0, For: 0}, AttemptsPerMinute: 0,
		Codes:      account.CodeRules{TTL: 0, Attempts: 0, Cooldown: 0, PerHour: 0},
		ReauthLock: account.Lock{After: 0, For: 0}, Locales: nil, Limits: limits,
	}

	return newHarness(t, func(c *identityhttp.Config) { c.Limits = limits }, identitytest.WithConfig(rules))
}

func address(n int) string { return strings.Repeat("e", n-len("@example.test")) + "@example.test" }

func accountBody(login, name, mail, phone string) map[string]string {
	return map[string]string{
		loginField: login, nameField: name, emailField: mail, phoneField: phone, localeField: "hr", passwordField: password,
	}
}

// A value between the host's width and the tag's is refused with the answer a tag's max gives.
func TestHostLimitsAnswerLikeATagsMax(t *testing.T) {
	t.Parallel()

	web := limited(t)
	ana := web.as("ana", secret)

	tag := web.do(nethttp.MethodPut, profilePath, map[string]string{nameField: strings.Repeat("n", 151)}, ana)
	require.Equal(t, nethttp.StatusUnprocessableEntity, tag.Code, tag.Body.String())

	host := web.do(nethttp.MethodPut, profilePath, map[string]string{nameField: strings.Repeat("n", 51)}, ana)
	require.Equal(t, nethttp.StatusUnprocessableEntity, host.Code, host.Body.String())
	assert.Equal(t, tag.Body.String(), host.Body.String())
}

func TestProfileHonoursTheHostLimits(t *testing.T) {
	t.Parallel()

	web := limited(t)
	ana := web.as("ana", secret)
	put := func(name, phone string) reply {
		return web.do(nethttp.MethodPut, profilePath, map[string]string{nameField: name, phoneField: phone}, ana)
	}

	assert.Equal(t, nethttp.StatusOK, put(strings.Repeat("n", 50), strings.Repeat("1", 20)).Code, "at the limit")

	for field, r := range map[string]reply{
		nameField: put(strings.Repeat("n", 51), "1"), phoneField: put("Ana", strings.Repeat("1", 21)),
	} {
		assert.Equal(t, nethttp.StatusUnprocessableEntity, r.Code, field)
		assert.Contains(t, r.Body.String(), `"`+field+`"`)
	}

	assert.Equal(t, nethttp.StatusOK, put(strings.Repeat("n", 50)+"  ", "1").Code, "the width is of the trimmed value")
}

func TestEmailChangeHonoursTheHostLimit(t *testing.T) {
	t.Parallel()

	web := limited(t)
	ana := web.as("ana", secret)
	request := func(mail string) reply {
		return web.do(nethttp.MethodPost, profilePath+"/email",
			map[string]string{emailField: mail, currentPass: secret}, ana)
	}

	r := request(address(51))
	assert.Equal(t, nethttp.StatusUnprocessableEntity, r.Code, r.Body.String())
	assert.Contains(t, r.Body.String(), `"`+emailField+`"`)
	assert.Less(t, request(address(50)).Code, 300, "at the limit")
}

func TestAdminHonoursTheHostLimits(t *testing.T) {
	t.Parallel()

	web := limited(t)
	ana := web.as("ana", secret)
	within := func(login, mail string) map[string]string {
		return accountBody(login, strings.Repeat("n", 50), mail, strings.Repeat("1", 20))
	}

	created := web.do(nethttp.MethodPost, usersPath, within(strings.Repeat("l", 15), address(50)), ana)
	require.Less(t, created.Code, 300, created.Body.String())

	for field, body := range map[string]map[string]string{
		loginField: accountBody(strings.Repeat("l", 16), "N", "n@example.test", ""),
		nameField:  accountBody("nn", strings.Repeat("n", 51), "n@example.test", ""),
		emailField: accountBody("nn", "N", address(51), ""),
		phoneField: accountBody("nn", "N", "n@example.test", strings.Repeat("1", 21)),
	} {
		for _, r := range []reply{
			web.do(nethttp.MethodPost, usersPath, body, ana), web.do(nethttp.MethodPut, usersPath+"/2", body, ana),
		} {
			assert.Equal(t, nethttp.StatusUnprocessableEntity, r.Code, field+" "+r.Body.String())
			assert.Contains(t, r.Body.String(), `"`+field+`"`)
		}
	}

	update := web.do(nethttp.MethodPut, usersPath+"/2", within("bor", "b@example.test"), ana)
	assert.Less(t, update.Code, 300, "an update at the limits")
}

// A login wider than the store keeps cannot exist: it is refused like any wrong sign-in.
func TestALoginWiderThanTheStoreIsAWrongSignIn(t *testing.T) {
	t.Parallel()

	web := limited(t)

	long := web.do(nethttp.MethodPost, "/v1/auth/login",
		map[string]string{loginField: strings.Repeat("l", 16), passwordField: "x"})
	wrong := web.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{loginField: "nobody", passwordField: "x"})

	assert.Equal(t, nethttp.StatusUnprocessableEntity, long.Code)
	assert.Equal(t, "identity.signin.failed", long.json()["code"])
	assert.Equal(t, wrong.Body.String(), long.Body.String())
	assert.Equal(t, nethttp.StatusOK, web.login().Code, "a login within the limit still signs in")
}

type loginAsNotices struct {
	account.DiscardNotices

	in, out []account.LoginAs
}

func (n *loginAsNotices) SignedInAs(_ context.Context, f account.LoginAs)  { n.in = append(n.in, f) }
func (n *loginAsNotices) SignedOutAs(_ context.Context, f account.LoginAs) { n.out = append(n.out, f) }

// The facts of signing in as somebody, and of returning, carry the request's device and address.
func TestLoginAsFactsCarryTheDeviceAndAddress(t *testing.T) {
	t.Parallel()

	heard := &loginAsNotices{DiscardNotices: account.DiscardNotices{}, in: nil, out: nil}
	web := newHarness(t, nil, identitytest.WithImpersonation(permitAll{}), identitytest.WithNotices(heard))
	own := web.login().cookie("access_token").Value

	agent := func(ua string) func(*nethttp.Request) {
		return func(r *nethttp.Request) { r.Header.Set("User-Agent", ua) }
	}
	firefox := "Mozilla/5.0 (Windows NT 10.0) Firefox/130.0"
	chrome := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/120.0 Safari/537.36"

	impersonated := web.do(nethttp.MethodPost, "/v1/auth/login-as", map[string]int{userIDField: 2},
		withCookie("access_token", own), agent(firefox))
	require.Equal(t, nethttp.StatusOK, impersonated.Code, impersonated.Body.String())
	require.Equal(t, []account.LoginAs{{ActorID: 1, TargetID: 2, Device: "Firefox · Windows", IP: "192.0.2.1"}}, heard.in)

	back := web.do(nethttp.MethodPost, "/v1/auth/login-as/return", nil,
		withCookie("access_token", impersonated.cookie("access_token").Value), agent(chrome))
	require.Equal(t, nethttp.StatusOK, back.Code, back.Body.String())
	require.Len(t, heard.out, 1)

	device := account.DeviceLabel(chrome)
	assert.NotEqual(t, "Firefox · Windows", device)
	assert.Equal(t, account.LoginAs{ActorID: 1, TargetID: 2, Device: device, IP: "192.0.2.1"}, heard.out[0])
}
