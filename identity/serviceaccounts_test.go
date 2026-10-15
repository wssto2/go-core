//nolint:exhaustruct,exhaustruct_v5,gochecknoglobals,goconst,ireturn,lll,nonamedreturns,paralleltest,tagliatelle,unparam,unqueryvet,varnamelen // tests in the style of the package's others
package identity_test

import (
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/identity/serviceaccount"
	"github.com/wssto2/go-core/route"
	"gorm.io/gorm"
)

const robotsPath = "/v1/iam/service-accounts"

// Customers is a route of the application that a role of customers' readers reaches.
var Customers = route.Get[route.None, string]("/customers").Requires("crm.customer:view")

// robotsApp is an application with people, service accounts and roles: user 1 is the webmaster, user 2 answers for the
// accounts, user 3 has no role.
type robotsApp struct {
	t       testing.TB
	app     *gocore.App
	db      *gorm.DB
	handler nethttp.Handler
	web     *client
}

func newRobots(t *testing.T, cfg serviceaccount.Config, opts ...identity.Option) *robotsApp {
	t.Helper()

	cat := authz.NewCatalogue()
	cat.MustDefine("crm.customer:view")

	app := gocoretest.New(t)
	users := identity.Install(app, append([]identity.Option{identity.WithoutMail(), identity.WithServiceAccounts(cfg)}, opts...)...)
	acc := access.Install(app, cat, users, access.WithRoles(
		authz.ComputedRole("webmaster", "Webmaster", authz.All()),
		authz.Role{Key: "reader", Name: "Reader", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")},
	))

	app.Routes(
		Customers.To(func(context.Context, route.None) (string, error) { return "customers", nil }),
		route.Get[route.None, string]("/whoami").Authenticated().To(func(ctx context.Context, _ route.None) (string, error) {
			who, _ := auth.UserFromContext(ctx)
			p, _ := authz.PrincipalFrom(ctx)

			return fmt.Sprintf("%s:%d:%d", p.Kind, p.ID, who.GetID()), nil
		}),
		route.Get[route.None, string]("/user").Authenticated().To(func(ctx context.Context, _ route.None) (string, error) {
			who, ok := auth.UserOf[stranger](ctx)
			if !ok {
				return "no stranger", nil
			}

			if who.Robot > 0 {
				return fmt.Sprintf("robot %d %s", who.Robot, who.Name), nil
			}

			return fmt.Sprintf("person %d %s", who.ID, who.Name), nil
		}),
		route.Post[route.None, string]("/orders").Authenticated().To(func(context.Context, route.None) (string, error) { return "ordered", nil }),
	)

	for id, login := range map[int]string{1: "webmaster", 2: "owner", 3: "nobody"} {
		seed(t, app.Database(), identitytest.Account(id, login, "secret"))
	}

	require.NoError(t, app.Check())
	require.NoError(t, acc.Seed(t.Context(), authz.Subject{Kind: authz.KindUser, ID: 1}, "webmaster"))

	handler, err := app.Handler()
	require.NoError(t, err)

	r := &robotsApp{t: t, app: app, db: app.Database(), handler: handler, web: newClient(t, app)}
	require.Equal(t, nethttp.StatusOK, r.web.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "webmaster", "password": "secret"}).Code)

	return r
}

// withKey makes the request as the service account the secret belongs to.
func (r *robotsApp) withKey(method, path, secret string, body any) *httptest.ResponseRecorder {
	r.t.Helper()

	reader := strings.NewReader("")

	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(r.t, err)

		reader = strings.NewReader(string(raw))
	}

	req := httptest.NewRequestWithContext(r.t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(identity.KeyHeader, secret)

	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)

	return rec
}

func data[T any](tb testing.TB, rec *httptest.ResponseRecorder) T {
	tb.Helper()

	var out struct {
		Data T `json:"data"`
	}

	require.NoError(tb, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())

	return out.Data
}

// account makes a service account owned by user 2 and gives it a key, returning the account and its secret.
func (r *robotsApp) account(name string) (id int, secret string) {
	r.t.Helper()

	made := r.web.do(nethttp.MethodPost, robotsPath, map[string]any{"name": name, "owner_id": 2})
	require.Equal(r.t, nethttp.StatusCreated, made.Code, made.Body.String())

	id = data[struct {
		ID int `json:"id"`
	}](r.t, made).ID

	key := r.web.do(nethttp.MethodPost, robotsPath+"/"+strconv.Itoa(id)+"/keys", map[string]any{"name": "ci"})
	require.Equal(r.t, nethttp.StatusCreated, key.Code, key.Body.String())
	assert.Equal(r.t, "no-store", key.Header().Get("Cache-Control"), "a secret is never cached")

	return id, data[struct {
		Secret string `json:"secret"`
	}](r.t, key).Secret
}

// The whole life of a service account: a person makes it, gives it a role and a key; the key calls the API as the account.
func TestServiceAccountCallsTheAPIWithItsKey(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{})
	id, secret := r.account("DMS export")

	assert.True(t, strings.HasPrefix(secret, serviceaccount.DefaultKeyPrefix), "the secret has the module's prefix")
	assert.Equal(t, nethttp.StatusForbidden, r.withKey(nethttp.MethodGet, "/customers", secret, nil).Code, "a new account holds no role")

	bound := r.web.do(nethttp.MethodPost, robotsPath+"/"+strconv.Itoa(id)+"/bindings", map[string]any{"role_ref": "reader", "level": "organization"})
	require.Equal(t, nethttp.StatusCreated, bound.Code, bound.Body.String())

	read := r.withKey(nethttp.MethodGet, "/customers", secret, nil)
	assert.Equal(t, nethttp.StatusOK, read.Code, read.Body.String())

	who := r.withKey(nethttp.MethodGet, "/whoami", secret, nil)
	assert.JSONEq(t, `{"success":true,"data":"service:`+strconv.Itoa(id)+`:0"}`, who.Body.String(), "the principal is the service account, and it is no user id")

	// the key was used
	got := data[struct {
		LastUsedAt *time.Time `json:"last_used_at"`
		ActiveKeys int        `json:"active_keys"`
	}](t, r.web.do(nethttp.MethodGet, robotsPath+"/"+strconv.Itoa(id), nil))
	require.NotNil(t, got.LastUsedAt, "the use is stamped")
	assert.Equal(t, 1, got.ActiveKeys)

	// the secret is not stored anywhere
	var rows []map[string]any
	require.NoError(t, r.db.Raw("SELECT * FROM service_account_keys").Scan(&rows).Error)
	require.Len(t, rows, 1)

	raw, err := json.Marshal(rows)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), secret)
	assert.Contains(t, string(raw), serviceaccount.HashSecret(secret))
}

// The refusals of a key: unknown, revoked, expired, of a deactivated account.
func TestAKeyThatCannotWorkIsRefused(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{})
	id, secret := r.account("feed")
	assert.Equal(t, nethttp.StatusOK, r.withKey(nethttp.MethodGet, "/whoami", secret, nil).Code)

	for name, bad := range map[string]string{"unknown": serviceaccount.DefaultKeyPrefix + "nope", "no prefix": "nothing-like-a-key", "too long": strings.Repeat("k", 300)} {
		rec := r.withKey(nethttp.MethodGet, "/whoami", bad, nil)
		assert.Equal(t, nethttp.StatusUnauthorized, rec.Code, name)
		assert.NotContains(t, rec.Body.String(), bad)
	}

	t.Run("expired", func(t *testing.T) {
		require.NoError(t, r.db.Exec("UPDATE service_account_keys SET expires_at = ?", gocoretest.Epoch.Add(-time.Hour)).Error)
		assert.Equal(t, nethttp.StatusUnauthorized, r.withKey(nethttp.MethodGet, "/whoami", secret, nil).Code)
		require.NoError(t, r.db.Exec("UPDATE service_account_keys SET expires_at = NULL").Error)
		assert.Equal(t, nethttp.StatusOK, r.withKey(nethttp.MethodGet, "/whoami", secret, nil).Code)
	})

	t.Run("revoked", func(t *testing.T) {
		keys := data[struct {
			Keys []struct {
				ID int `json:"id"`
			} `json:"keys"`
		}](t, r.web.do(nethttp.MethodGet, robotsPath+"/"+strconv.Itoa(id)+"/keys", nil))
		require.Len(t, keys.Keys, 1)

		revoke := r.web.do(nethttp.MethodPost, robotsPath+"/"+strconv.Itoa(id)+"/keys/"+strconv.Itoa(keys.Keys[0].ID)+"/revoke", nil)
		require.Equal(t, nethttp.StatusNoContent, revoke.Code)
		assert.Equal(t, nethttp.StatusUnauthorized, r.withKey(nethttp.MethodGet, "/whoami", secret, nil).Code, "it stops at once")
	})

	t.Run("of a deactivated account", func(t *testing.T) {
		id, secret := r.account("another feed")
		require.Equal(t, nethttp.StatusOK, r.withKey(nethttp.MethodGet, "/whoami", secret, nil).Code)

		off := r.web.do(nethttp.MethodPost, robotsPath+"/"+strconv.Itoa(id)+"/deactivate", nil)
		require.Equal(t, nethttp.StatusOK, off.Code, off.Body.String())
		assert.Equal(t, nethttp.StatusUnauthorized, r.withKey(nethttp.MethodGet, "/whoami", secret, nil).Code)
	})
}

// What is a person's is refused to a key, and what a key changes leaves a row in the audit trail.
func TestAKeyIsRefusedPersonRoutesAndItsWritesAreAudited(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{})
	id, secret := r.account("writer")

	for _, path := range []string{"/v1/auth/me", "/v1/iam/profile", "/v1/iam/profile/sessions"} {
		rec := r.withKey(nethttp.MethodGet, path, secret, nil)
		assert.Equal(t, nethttp.StatusForbidden, rec.Code, path)
		assert.Contains(t, rec.Body.String(), "person_only", path)
	}

	var before int64

	require.NoError(t, r.db.Table("audit_logs").Where("entity_type = ? AND action = ?", serviceaccount.AccountEntity, "request").Count(&before).Error)

	assert.Equal(t, nethttp.StatusOK, r.withKey(nethttp.MethodGet, "/whoami", secret, nil).Code)

	var afterRead int64

	require.NoError(t, r.db.Table("audit_logs").Where("entity_type = ? AND action = ?", serviceaccount.AccountEntity, "request").Count(&afterRead).Error)
	assert.Equal(t, before, afterRead, "a read leaves no row")

	assert.Equal(t, nethttp.StatusOK, r.withKey(nethttp.MethodPost, "/orders", secret, nil).Code)

	var rows []struct {
		EntityID   int
		Action     string
		AfterState string
	}

	require.NoError(t, r.db.Table("audit_logs").Where("entity_type = ? AND action = ?", serviceaccount.AccountEntity, "request").Scan(&rows).Error)
	require.Len(t, rows, 1, "a write leaves one")
	assert.Equal(t, id, rows[0].EntityID, "naming the account")
	assert.Contains(t, rows[0].AfterState, `"method":"POST"`)
	assert.Contains(t, rows[0].AfterState, `"route":"/orders"`)
	assert.Contains(t, rows[0].AfterState, `"key_id":`, "and the key")
	assert.NotContains(t, rows[0].AfterState, secret)
}

// A person cannot be made a service account's by a service account: only a person manages them.
func TestAServiceAccountDoesNotMintKeys(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{})
	id, secret := r.account("greedy")

	bound := r.web.do(nethttp.MethodPost, robotsPath+"/"+strconv.Itoa(id)+"/bindings", map[string]any{"role_ref": "webmaster", "level": "organization"})
	require.Equal(t, nethttp.StatusCreated, bound.Code, bound.Body.String())

	rec := r.withKey(nethttp.MethodPost, robotsPath+"/"+strconv.Itoa(id)+"/keys", secret, map[string]any{"name": "more"})
	assert.Equal(t, nethttp.StatusForbidden, rec.Code, "even holding every permission")
	assert.Contains(t, rec.Body.String(), "person_only")
	assert.Empty(t, rec.Header().Get("Cache-Control"))
}

// A cookie of a session does not mix with a key: the key is the credential of the request.
func TestAKeyWinsOverASessionOnTheSameRequest(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{})
	_, secret := r.account("both")

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/whoami", nil)
	req.Header.Set(identity.KeyHeader, secret)

	for _, ck := range r.web.cookies {
		req.AddCookie(ck)
	}

	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)

	assert.Contains(t, rec.Body.String(), "service:", "the request is the service account's, not the webmaster's")
}

// stranger is an application's own user type, of a person and of a service account alike.
type stranger struct {
	ID    int
	Robot int
	Name  string
}

func (s stranger) GetID() int { return s.ID }

// WithRequestUser makes the request's user of a key's caller too: what it returns is the user, whoever it is for.
func TestRequestUserRunsForAKeyToo(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{}, identity.WithRequestUser(
		func(_ context.Context, who identity.Authenticated) (auth.Identifiable, error) {
			if who.ServiceAccount != nil {
				return stranger{ID: 0, Robot: who.ServiceAccount.AccountID, Name: who.ServiceAccount.Name}, nil
			}

			return stranger{ID: who.Account.ID, Robot: 0, Name: who.Account.Login}, nil
		}))

	id, secret := r.account("feed")

	byKey := r.withKey(nethttp.MethodGet, "/user", secret, nil)
	require.Equal(t, nethttp.StatusOK, byKey.Code, byKey.Body.String())
	assert.JSONEq(t, `{"success":true,"data":"robot `+strconv.Itoa(id)+` feed"}`, byKey.Body.String(), "the key's caller is the application's user")

	asPerson := r.web.do(nethttp.MethodGet, "/user", nil)
	assert.JSONEq(t, `{"success":true,"data":"person 1 webmaster"}`, asPerson.Body.String())

	// the principal is not the function's to change
	who := r.withKey(nethttp.MethodGet, "/whoami", secret, nil)
	assert.Contains(t, who.Body.String(), "service:"+strconv.Itoa(id))
}

// An error from the function answers a key request too.
func TestRequestUserCanRefuseAKey(t *testing.T) {
	r := newRobots(t, serviceaccount.Config{}, identity.WithRequestUser(
		func(_ context.Context, who identity.Authenticated) (auth.Identifiable, error) {
			if who.ServiceAccount != nil {
				return nil, apperr.Unauthorized("this robot is retired")
			}

			return who.Account, nil
		}))

	_, secret := r.account("feed")
	assert.Equal(t, nethttp.StatusUnauthorized, r.withKey(nethttp.MethodGet, "/whoami", secret, nil).Code)
}
