//nolint:goconst,lll,musttag,paralleltest,tagliatelle,varnamelen // tests in the style of the package's others
package identity_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/route"
)

// embedSecret is the secret of arv-next's wire pin, so its tokens are valid here.
const embedSecret = "embed-secret-for-the-wire-pin-0123456789"

// farFuture is 2100-01-01.
const farFuture = 4102444800

func hostToken(t *testing.T, claims map[string]any, secret string) string {
	t.Helper()

	enc := base64.RawURLEncoding
	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	body := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + enc.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))

	return body + "." + enc.EncodeToString(mac.Sum(nil))
}

func embedApp(t *testing.T) (func(query string) *httptest.ResponseRecorder, *client) {
	t.Helper()

	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithEmbed(embedSecret))
	app.Routes(route.Get[route.None, string]("/shop").To(shopOf))

	ana := identitytest.Account(7, "ana", "secret")
	seed(t, app.Database(), ana)

	inactive := identitytest.Account(9, "inactive", "secret")
	inactive.Active = false
	seed(t, app.Database(), inactive)

	handler, err := app.Handler()
	require.NoError(t, err)

	get := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/embed"+query, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Safari/604.1")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		return rec
	}

	return get, newClient(t, app)
}

var sessionPattern = regexp.MustCompile(`sessionStorage\.setItem\('embed_access_token',"([^"]+)"\);\nsessionStorage\.setItem\('embed_expires_at',"([^"]+)"\);`)

// The wire of arv-next's pin (testdata/arv_wire_embed.json): the same tokens, the same answers, bar the access token
// and the time the session ends, which are not the same on every run.
func TestEmbedAnswersAsArvNextPinned(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "arv_wire_embed.json"))
	require.NoError(t, err)

	var pinned map[string]struct {
		Request  struct{ Path string }
		Response struct {
			Status      int
			ContentType string `json:"content_type"`
			Body        any
		}
	}

	require.NoError(t, json.Unmarshal(raw, &pinned))
	require.Len(t, pinned, 10)

	get, _ := embedApp(t)

	// the cases depend on each other (a token works once), so they run in the order arv-next made them
	for _, name := range []string{
		"no token", "not a token", "wrong signature", "expired", "no jti", "subject is not a number", "unknown user", "ok", "replayed", "ok without redirect",
	} {
		want := pinned[name]
		rec := get(strings.TrimPrefix(want.Request.Path, "/embed"))

		assert.Equal(t, want.Response.Status, rec.Code, name)
		assert.Equal(t, want.Response.ContentType, rec.Header().Get("Content-Type"), name)

		if text, isText := want.Response.Body.(string); isText {
			got := sessionPattern.ReplaceAllString(rec.Body.String(),
				"sessionStorage.setItem('embed_access_token',<redacted>);\nsessionStorage.setItem('embed_expires_at',<redacted>);")
			assert.Equal(t, strings.ReplaceAll(text, `nonce="NONCE"`, `nonce=""`), got, name) // no CSP middleware in a test: no nonce
		} else {
			assert.JSONEq(t, asJSONText(t, want.Response.Body), rec.Body.String(), name)
		}
	}
}

func asJSONText(t *testing.T, v any) string {
	t.Helper()

	raw, err := json.Marshal(v)
	require.NoError(t, err)

	return string(raw)
}

// The session the page keeps is a session: 24 hours, no refresh, good for identity's routes, and the sign-in is recorded.
func TestEmbedOpensASessionOfADayThatCannotBeRefreshed(t *testing.T) {
	get, web := embedApp(t)

	rec := get("?token=" + hostToken(t, map[string]any{"sub": "7", "jti": "once", "exp": farFuture}, embedSecret) + "&redirect=%2Fleads%3Fa%3D1")
	require.Equal(t, nethttp.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), "the page holds a credential")
	assert.Empty(t, rec.Result().Cookies(), "no cookie: the page keeps the token itself")

	found := sessionPattern.FindStringSubmatch(rec.Body.String())
	require.Len(t, found, 3)
	assert.Contains(t, rec.Body.String(), `window.location.replace("/leads?a=1");`)

	expires, err := time.Parse(time.RFC3339, found[2])
	require.NoError(t, err)
	assert.Equal(t, gocoretest.Epoch.Add(24*time.Hour), expires)

	// the access token is a bearer token for every route
	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/shop", nil)
	req.Header.Set("Authorization", "Bearer "+found[1])

	got := httptest.NewRecorder()
	web.handler.ServeHTTP(got, req)
	assert.Equal(t, nethttp.StatusOK, got.Code, got.Body.String())

	// it opened one session, which has a refresh token nobody holds: no response carries it
	assert.NotContains(t, rec.Body.String(), "refresh")
}

// redirect is a path of this site; anything that would leave it is "/", and the person is signed in all the same.
func TestEmbedRedirectsOnlyWithinTheSite(t *testing.T) {
	get, _ := embedApp(t)

	for i, tc := range []struct{ redirect, want string }{
		{"/leads", "/leads"}, {"/a/b?c=d#e", "/a/b?c=d#e"}, {"", "/"},
		{"//evil.example", "/"}, {"https://evil.example/", "/"}, {"/\\evil.example", "/"}, {"evil.example", "/"},
		{"javascript:alert(1)", "/"}, {"/x\ny", "/"}, {"\\\\evil.example", "/"},
	} {
		token := hostToken(t, map[string]any{"sub": "7", "jti": "r" + strconv.Itoa(i), "exp": farFuture}, embedSecret)
		rec := get("?token=" + token + "&redirect=" + url.QueryEscape(tc.redirect))

		require.Equal(t, nethttp.StatusOK, rec.Code, tc.redirect)

		wantJSON, err := json.Marshal(tc.want)
		require.NoError(t, err)
		assert.Contains(t, rec.Body.String(), "window.location.replace("+string(wantJSON)+");", "redirect %q", tc.redirect)
	}
}

func TestEmbedRefusesWhoCannotSignIn(t *testing.T) {
	get, _ := embedApp(t)

	for name, sub := range map[string]string{"deactivated": "9", "unknown": "404"} {
		rec := get("?token=" + hostToken(t, map[string]any{"sub": sub, "jti": name, "exp": farFuture}, embedSecret))
		assert.Equal(t, nethttp.StatusUnauthorized, rec.Code, name)
		assert.JSONEq(t, `{"error":"unauthorized"}`, rec.Body.String(), name)
	}
}

func TestEmbedNeedsALongSecret(t *testing.T) {
	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithEmbed("short"))

	err := app.Check()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "embed secret")

	app = newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithEmbed(""))
	require.Error(t, app.Check(), "an unset variable is not a switched-off embed")
}
