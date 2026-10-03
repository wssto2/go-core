package http_test

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/identity/account"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/navigation"
	"github.com/wssto2/go-core/route"
	"log/slog"
)

type permitAll struct{}

func (permitAll) Permitted(context.Context, account.Account) error               { return nil }
func (permitAll) Covers(context.Context, account.Account, account.Account) error { return nil }

// harness serves the identity routes over memory stores.
type harness struct {
	t      *testing.T
	kit    identitytest.Kit
	engine *gin.Engine
}

func newHarness(t *testing.T, mutate func(*identityhttp.Config), opts ...identitytest.Option) *harness {
	return newHarnessAt(t, "", mutate, opts...)
}

// newHarnessAt mounts the routes below an application prefix, as gocore.WithPrefix does.
func newHarnessAt(t *testing.T, prefix string, mutate func(*identityhttp.Config), opts ...identitytest.Option) *harness {
	t.Helper()

	inactive := identitytest.Account(3, "ines", "secret")
	inactive.Active = false
	ana := identitytest.Account(1, "ana", "secret")
	ana.Name, ana.Email, ana.Locale = "Ana Anić", "ana@example.test", "hr"

	kit := identitytest.New(t, []account.Account{ana, identitytest.Account(2, "boris", "hunter2"), inactive}, opts...)
	cfg := identityhttp.Config{Services: account.Services{SignIn: kit.SignIn, Users: kit.Users}, Clock: kit.Clock}

	if mutate != nil {
		mutate(&cfg)
	}

	handler := identityhttp.NewHandler(cfg)

	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, true))

	security := route.Security{Authenticate: []gin.HandlerFunc{identityhttp.Authentication(kit.SignIn, cfg.Cookies, cfg.Principal)}}
	var routes gin.IRoutes = engine
	if prefix != "" {
		routes = engine.Group(prefix)
	}

	for _, r := range handler.Routes() {
		require.NoError(t, r.Mount(routes, security))
	}

	return &harness{t: t, kit: kit, engine: engine}
}

type reply struct {
	*httptest.ResponseRecorder
}

func (h *harness) do(method, path string, body any, mod ...func(*nethttp.Request)) reply {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(h.t, err)

		reader = strings.NewReader(string(raw))
	}

	req := httptest.NewRequestWithContext(h.t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "test-agent")

	for _, m := range mod {
		m(req)
	}

	rec := httptest.NewRecorder()
	h.engine.ServeHTTP(rec, req)

	return reply{rec}
}

func (r reply) cookie(name string) *nethttp.Cookie {
	for _, c := range r.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}

	return nil
}

func (r reply) json() map[string]any {
	var out map[string]any

	_ = json.Unmarshal(r.Body.Bytes(), &out)

	return out
}

func withCookie(name, value string) func(*nethttp.Request) {
	return func(r *nethttp.Request) {
		r.AddCookie(&nethttp.Cookie{Name: name, Value: value}) //nolint:gosec // a request cookie in a test
	}
}

func (h *harness) login() reply {
	h.t.Helper()

	r := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"})
	require.Equal(h.t, nethttp.StatusOK, r.Code, r.Body.String())

	return r
}

func TestLoginAnswersThePayloadAndSetsTheCookies(t *testing.T) {
	h := newHarness(t, nil)

	r := h.login()

	assert.JSONEq(t, `{"success":true,"data":{
		"user":{"id":1,"login":"ana","name":"Ana Anić","email":"ana@example.test","locale":"hr"},
		"expires_at":"2026-01-03T03:04:05Z",
		"access":{"subject":{"kind":"user","id":1},"root":false,"permissions":{}}
	}}`, r.Body.String())

	access, refresh := r.cookie("access_token"), r.cookie("refresh_token")
	require.NotNil(t, access)
	require.NotNil(t, refresh)

	assert.True(t, access.HttpOnly)
	assert.Equal(t, "/", access.Path)
	assert.Equal(t, 24*3600, access.MaxAge)
	assert.Equal(t, nethttp.SameSiteLaxMode, access.SameSite)
	assert.Equal(t, "/v1/auth/refresh", refresh.Path, "the refresh cookie goes to the refresh route only")
	assert.Equal(t, 48*3600, refresh.MaxAge)
	assert.NotContains(t, r.Body.String(), access.Value, "the tokens are cookies, not payload")
	assert.NotContains(t, r.Body.String(), "password")
}

func TestSecureCookiesFollowTheRequest(t *testing.T) {
	h := newHarness(t, nil)

	r := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"},
		func(r *nethttp.Request) { r.Header.Set("X-Forwarded-Proto", "https") })

	assert.True(t, r.cookie("access_token").Secure)
}

// IAM-USER-001 on the wire: one answer for an unknown login and a wrong password.
func TestFailedSignInsAnswerAlike(t *testing.T) {
	h := newHarness(t, nil)

	unknown := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "nobody", "password": "x"})
	wrong := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "x"})

	for _, r := range []reply{unknown, wrong} {
		assert.Equal(t, nethttp.StatusUnprocessableEntity, r.Code)
		assert.Equal(t, "identity.signin.failed", r.json()["code"])
		assert.Nil(t, r.cookie("access_token"))
	}

	assert.Equal(t, unknown.Body.String(), wrong.Body.String())

	inactive := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ines", "password": "secret"})
	assert.Equal(t, nethttp.StatusBadRequest, inactive.Code)
	assert.Equal(t, "identity.signin.inactive", inactive.json()["code"])
}

func TestTheLockAnswersWhenItEnds(t *testing.T) {
	h := newHarness(t, nil)

	for range 5 {
		h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "x"})
	}

	r := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"})
	assert.Equal(t, nethttp.StatusUnprocessableEntity, r.Code)
	assert.Equal(t, "identity.signin.locked", r.json()["code"])
	assert.Equal(t, map[string]any{"locked_until": "2026-01-02T03:19:05Z"}, r.json()["params"])
}

func TestInputIsValidated(t *testing.T) {
	h := newHarness(t, nil)

	assert.Equal(t, nethttp.StatusUnprocessableEntity, h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "", "password": "x"}).Code)
	assert.Equal(t, nethttp.StatusUnprocessableEntity,
		h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": strings.Repeat("a", 101), "password": "x"}).Code)
}

func TestMeReadsTheCookieOrTheBearerToken(t *testing.T) {
	h := newHarness(t, nil)
	login := h.login()
	token := login.cookie("access_token").Value

	byCookie := h.do(nethttp.MethodGet, "/v1/auth/me", nil, withCookie("access_token", token))
	assert.Equal(t, nethttp.StatusOK, byCookie.Code)

	byBearer := h.do(nethttp.MethodGet, "/v1/auth/me", nil, func(r *nethttp.Request) { r.Header.Set("Authorization", "Bearer "+token) })
	assert.Equal(t, nethttp.StatusOK, byBearer.Code)
	assert.JSONEq(t, byCookie.Body.String(), byBearer.Body.String())

	data := byCookie.json()["data"].(map[string]any)
	assert.Equal(t, "ana", data["user"].(map[string]any)["login"])
	assert.Equal(t, "2026-01-03T03:04:05Z", data["expires_at"])
}

func TestEveryRouteButLoginAndRefreshNeedsASession(t *testing.T) {
	h := newHarness(t, nil)

	for _, c := range []struct{ method, path string }{
		{nethttp.MethodGet, "/v1/auth/me"},
		{nethttp.MethodPost, "/v1/auth/logout"},
		{nethttp.MethodPost, "/v1/auth/change-locale"},
		{nethttp.MethodPost, "/v1/auth/login-as"},
	} {
		r := h.do(c.method, c.path, map[string]any{"locale": "en", "user_id": 2})
		assert.Equal(t, nethttp.StatusUnauthorized, r.Code, c.path)
		assert.Equal(t, "identity.session.invalid", r.json()["code"], c.path)
	}

	bad := h.do(nethttp.MethodGet, "/v1/auth/me", nil, withCookie("access_token", "nope"))
	assert.Equal(t, nethttp.StatusUnauthorized, bad.Code)
}

func TestRefreshRotatesTheTokens(t *testing.T) {
	h := newHarness(t, nil)
	first := h.login()
	oldAccess, oldRefresh := first.cookie("access_token").Value, first.cookie("refresh_token").Value

	r := h.do(nethttp.MethodPost, "/v1/auth/refresh", nil, withCookie("refresh_token", oldRefresh))
	require.Equal(t, nethttp.StatusOK, r.Code, r.Body.String())
	assert.NotEqual(t, oldAccess, r.cookie("access_token").Value)
	assert.NotEqual(t, oldRefresh, r.cookie("refresh_token").Value)

	again := h.do(nethttp.MethodPost, "/v1/auth/refresh", nil, withCookie("refresh_token", oldRefresh))
	assert.Equal(t, nethttp.StatusUnauthorized, again.Code, "a refresh token works once")

	fromBody := h.do(nethttp.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": r.cookie("refresh_token").Value})
	assert.Equal(t, nethttp.StatusOK, fromBody.Code, "the token may come in the body")

	none := h.do(nethttp.MethodPost, "/v1/auth/refresh", nil)
	assert.Equal(t, nethttp.StatusUnauthorized, none.Code)
}

func TestLogoutEndsTheSessionAndClearsTheCookies(t *testing.T) {
	h := newHarness(t, nil)
	token := h.login().cookie("access_token").Value

	r := h.do(nethttp.MethodPost, "/v1/auth/logout", nil, withCookie("access_token", token))
	assert.Equal(t, nethttp.StatusNoContent, r.Code)
	assert.Equal(t, -1, r.cookie("access_token").MaxAge)
	assert.Equal(t, -1, r.cookie("refresh_token").MaxAge)

	assert.Equal(t, nethttp.StatusUnauthorized, h.do(nethttp.MethodGet, "/v1/auth/me", nil, withCookie("access_token", token)).Code)
}

func TestChangeLocale(t *testing.T) {
	h := newHarness(t, nil)
	token := h.login().cookie("access_token").Value

	r := h.do(nethttp.MethodPost, "/v1/auth/change-locale", map[string]string{"locale": "en"}, withCookie("access_token", token))
	assert.Equal(t, nethttp.StatusNoContent, r.Code)

	me := h.do(nethttp.MethodGet, "/v1/auth/me", nil, withCookie("access_token", token))
	assert.Equal(t, "en", me.json()["data"].(map[string]any)["user"].(map[string]any)["locale"])

	bad := h.do(nethttp.MethodPost, "/v1/auth/change-locale", map[string]string{"locale": "Croatian"}, withCookie("access_token", token))
	assert.Equal(t, nethttp.StatusBadRequest, bad.Code)
	assert.Equal(t, "identity.locale.invalid", bad.json()["code"])
}

func TestLoginAsIsRefusedUnlessTheApplicationAllowsIt(t *testing.T) {
	h := newHarness(t, nil)
	token := h.login().cookie("access_token").Value

	r := h.do(nethttp.MethodPost, "/v1/auth/login-as", map[string]int{"user_id": 2}, withCookie("access_token", token))
	assert.Equal(t, nethttp.StatusForbidden, r.Code)
	assert.Equal(t, "identity.impersonation.disabled", r.json()["code"])
}

func TestLoginAsSignsInAsTheTarget(t *testing.T) {
	h := newHarness(t, nil, identitytest.WithImpersonation(permitAll{}))
	token := h.login().cookie("access_token").Value

	r := h.do(nethttp.MethodPost, "/v1/auth/login-as", map[string]int{"user_id": 2}, withCookie("access_token", token))
	require.Equal(t, nethttp.StatusOK, r.Code, r.Body.String())
	assert.Equal(t, "boris", r.json()["data"].(map[string]any)["user"].(map[string]any)["login"])

	asBoris := r.cookie("access_token").Value
	me := h.do(nethttp.MethodGet, "/v1/auth/me", nil, withCookie("access_token", asBoris))
	assert.Equal(t, "boris", me.json()["data"].(map[string]any)["user"].(map[string]any)["login"])
}

func TestThePayloadCarriesAccessAndTheFilteredMenu(t *testing.T) {
	held := authz.MyAccess{
		Subject: authz.Subject{Kind: authz.KindUser, ID: 1},
		Permissions: map[string]authz.PermissionAccess{
			"tickets.ticket:view": {Scope: authz.Scope{Level: "organization"}, Qualifier: authz.QualifierAll},
		},
	}

	h := newHarness(t, func(c *identityhttp.Config) {
		c.Access = func(ctx context.Context) (authz.MyAccess, error) {
			p, ok := authz.PrincipalFrom(ctx)
			assert.True(t, ok, "the engine acts as the signed-in person, even at login")
			assert.Equal(t, 1, p.ID)

			return held, nil
		}
		c.Navigation = func(context.Context, account.Account) ([]navigation.Node, error) {
			return []navigation.Node{
				{I18n: "nav.tickets", Route: "tickets.index", Permissions: []string{"tickets.ticket:view"}},
				{I18n: "nav.roles", Route: "roles.index", Permissions: []string{"access.role:view"}},
			}, nil
		}
	})

	data := h.login().json()["data"].(map[string]any)

	assert.Equal(t, []any{map[string]any{"i18n": "nav.tickets", "route": "tickets.index", "permissions": []any{"tickets.ticket:view"}}}, data["navigation"])
	assert.Contains(t, data["access"].(map[string]any)["permissions"], "tickets.ticket:view")
}

func TestTheUserProjectionIsTheApplications(t *testing.T) {
	h := newHarness(t, func(c *identityhttp.Config) {
		c.Project = func(_ context.Context, a account.Account) (any, error) {
			return map[string]any{"id": a.ID, "shout": strings.ToUpper(a.Login)}, nil
		}
	})

	assert.Equal(t, map[string]any{"id": float64(1), "shout": "ANA"}, h.login().json()["data"].(map[string]any)["user"])
}

func TestTheRefreshCookieFollowsWhereTheRoutesAreServed(t *testing.T) {
	h := newHarnessAt(t, "/api", nil)

	r := h.do(nethttp.MethodPost, "/api/v1/auth/login", map[string]string{"login": "ana", "password": "secret"})
	require.Equal(t, nethttp.StatusOK, r.Code)
	assert.Equal(t, "/api/v1/auth/refresh", r.cookie("refresh_token").Path)
	assert.Equal(t, "/", r.cookie("access_token").Path)

	refreshed := h.do(nethttp.MethodPost, "/api/v1/auth/refresh", nil, withCookie("refresh_token", r.cookie("refresh_token").Value))
	require.Equal(t, nethttp.StatusOK, refreshed.Code)
	assert.Equal(t, "/api/v1/auth/refresh", refreshed.cookie("refresh_token").Path)

	out := h.do(nethttp.MethodPost, "/api/v1/auth/logout", nil, withCookie("access_token", refreshed.cookie("access_token").Value))
	assert.Equal(t, "/api/v1/auth/refresh", out.cookie("refresh_token").Path, "cleared where it was set")
	assert.Equal(t, nethttp.StatusNotFound, h.do(nethttp.MethodPost, "/v1/auth/login", nil).Code)
}

func TestRefreshPathCanBeSetForAProxy(t *testing.T) {
	h := newHarness(t, func(c *identityhttp.Config) { c.Cookies = identityhttp.Cookies{RefreshPath: "/public/refresh"} })

	assert.Equal(t, "/public/refresh", h.login().cookie("refresh_token").Path)
}

func TestCookieNamesAreConfigurable(t *testing.T) {
	h := newHarness(t, func(c *identityhttp.Config) {
		c.Cookies = identityhttp.Cookies{Access: "sid", Refresh: "rid", Domain: "example.test"}
	})

	r := h.login()
	require.NotNil(t, r.cookie("sid"))
	assert.Equal(t, "example.test", r.cookie("sid").Domain)
	assert.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodGet, "/v1/auth/me", nil, withCookie("sid", r.cookie("sid").Value)).Code)
}

func TestAuthenticationLeavesTheIdentityForOtherRoutes(t *testing.T) {
	h := newHarness(t, nil)
	token := h.login().cookie("access_token").Value

	var who account.Authenticated

	var principal authz.Principal

	probe := route.Get[route.None, route.Empty]("/probe")
	require.NoError(t, probe.To(func(ctx context.Context, _ route.None) (route.Empty, error) {
		who, _ = identityhttp.AuthenticatedFrom(ctx)
		principal, _ = authz.PrincipalFrom(ctx)

		return route.Empty{}, nil
	}).Mount(h.engine, route.Security{Authenticate: []gin.HandlerFunc{identityhttp.Authentication(h.kit.SignIn, identityhttp.Cookies{}, nil)}}))

	r := h.do(nethttp.MethodGet, "/probe", nil, withCookie("access_token", token))
	require.Equal(t, nethttp.StatusNoContent, r.Code)
	assert.Equal(t, 1, who.Account.ID)
	assert.Equal(t, authz.User(1, 0), principal)
}

func TestTheSessionIsTouchedAsTimePasses(t *testing.T) {
	h := newHarness(t, nil)
	token := h.login().cookie("access_token").Value

	h.kit.Clock.Advance(2 * time.Hour)

	assert.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodGet, "/v1/auth/me", nil, withCookie("access_token", token)).Code)

	sessions, err := h.kit.Users.Sessions(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, h.kit.Clock.Now(), sessions[0].LastUsedAt)
}
