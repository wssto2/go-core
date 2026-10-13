package http_test

import (
	nethttp "net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/identity/account"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/identitytest"
)

var narrow = account.Limits{Login: 15, Name: 50, Email: 50, Phone: 20}

func limited(t *testing.T) *harness {
	return newHarness(t, func(c *identityhttp.Config) { c.Limits = narrow }, identitytest.WithConfig(account.Config{Limits: narrow}))
}

func email(n int) string { return strings.Repeat("e", n-len("@example.test")) + "@example.test" }

// A value between the host's width and the tag's is refused with the answer a tag's max gives.
func TestHostLimitsAnswerLikeATagsMax(t *testing.T) {
	h := limited(t)
	ana := h.as("ana", "secret")

	tag := h.do(nethttp.MethodPut, "/v1/iam/profile", map[string]string{"name": strings.Repeat("n", 151)}, ana)
	require.Equal(t, nethttp.StatusUnprocessableEntity, tag.Code, tag.Body.String())

	host := h.do(nethttp.MethodPut, "/v1/iam/profile", map[string]string{"name": strings.Repeat("n", 51)}, ana)
	require.Equal(t, nethttp.StatusUnprocessableEntity, host.Code, host.Body.String())
	assert.Equal(t, strings.ReplaceAll(tag.Body.String(), "150", "50"), host.Body.String())
}

func TestProfileHonoursTheHostLimits(t *testing.T) {
	h := limited(t)
	ana := h.as("ana", "secret")
	put := func(name, phone string) reply {
		return h.do(nethttp.MethodPut, "/v1/iam/profile", map[string]string{"name": name, "phone": phone}, ana)
	}

	assert.Equal(t, nethttp.StatusOK, put(strings.Repeat("n", 50), strings.Repeat("1", 20)).Code, "at the limit")

	for field, r := range map[string]reply{"name": put(strings.Repeat("n", 51), "1"), "phone": put("Ana", strings.Repeat("1", 21))} {
		assert.Equal(t, nethttp.StatusUnprocessableEntity, r.Code, field)
		assert.Contains(t, r.Body.String(), `"`+field+`"`)
	}

	assert.Equal(t, nethttp.StatusOK, put(strings.Repeat("n", 50)+"  ", "1").Code, "the width is of the trimmed value")
}

func TestEmailChangeHonoursTheHostLimit(t *testing.T) {
	h := limited(t)
	ana := h.as("ana", "secret")
	request := func(e string) reply {
		return h.do(nethttp.MethodPost, "/v1/iam/profile/email", map[string]string{"email": e, "current_password": "secret"}, ana)
	}

	r := request(email(51))
	assert.Equal(t, nethttp.StatusUnprocessableEntity, r.Code, r.Body.String())
	assert.Contains(t, r.Body.String(), `"email"`)
	assert.Less(t, request(email(50)).Code, 300, "at the limit")
}

func TestAdminHonoursTheHostLimits(t *testing.T) {
	h := limited(t)
	ana := h.as("ana", "secret")

	body := func(login, name, e, phone string) map[string]string {
		return map[string]string{"login": login, "name": name, "email": e, "phone": phone, "locale": "hr", "password": "a long password"}
	}
	ok := func(l string) map[string]string {
		return body(l, strings.Repeat("n", 50), email(50), strings.Repeat("1", 20))
	}

	created := h.do(nethttp.MethodPost, "/v1/iam/users", ok(strings.Repeat("l", 15)), ana)
	require.Less(t, created.Code, 300, created.Body.String())

	for field, in := range map[string]map[string]string{
		"login": body(strings.Repeat("l", 16), "N", "n@example.test", ""),
		"name":  body("nn", strings.Repeat("n", 51), "n@example.test", ""),
		"email": body("nn", "N", email(51), ""),
		"phone": body("nn", "N", "n@example.test", strings.Repeat("1", 21)),
	} {
		for _, r := range []reply{h.do(nethttp.MethodPost, "/v1/iam/users", in, ana), h.do(nethttp.MethodPut, "/v1/iam/users/2", in, ana)} {
			assert.Equal(t, nethttp.StatusUnprocessableEntity, r.Code, field+" "+r.Body.String())
			assert.Contains(t, r.Body.String(), `"`+field+`"`)
		}
	}

	assert.Less(t, h.do(nethttp.MethodPut, "/v1/iam/users/2", withEmail(ok("bor"), "b@example.test"), ana).Code, 300, "an update at the limits")
}

// A login wider than the store keeps cannot exist: it is refused like any wrong sign-in.
func TestALoginWiderThanTheStoreIsAWrongSignIn(t *testing.T) {
	h := limited(t)

	long := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": strings.Repeat("l", 16), "password": "x"})
	wrong := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "nobody", "password": "x"})

	assert.Equal(t, nethttp.StatusUnprocessableEntity, long.Code)
	assert.Equal(t, "identity.signin.failed", long.json()["code"])
	assert.Equal(t, wrong.Body.String(), long.Body.String())
	assert.Equal(t, nethttp.StatusOK, h.login().Code, "a login within the limit still signs in")
}

func withEmail(m map[string]string, e string) map[string]string {
	m["email"] = e

	return m
}
