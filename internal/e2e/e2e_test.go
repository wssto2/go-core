// Package e2e holds the test that installs go-core's modules together, the way
// an application's main does, and uses them through HTTP.
package e2e_test

import (
	"context"
	"encoding/json"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/access/accesshttp"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/mail"
	"gorm.io/gorm"
)

// browser is an HTTP client that keeps cookies, like one.
type browser struct {
	t       *testing.T
	handler nethttp.Handler
	cookies map[string]*nethttp.Cookie
}

func (b *browser) do(method, path string, body any) (int, json.RawMessage) {
	b.t.Helper()

	reader := strings.NewReader("")

	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(b.t, err)

		reader = strings.NewReader(string(raw))
	}

	req := httptest.NewRequestWithContext(b.t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	for _, c := range b.cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)

	return rec.Code, envelope.Data
}

func (b *browser) signIn(login, password string) {
	b.t.Helper()

	status, data := b.do(nethttp.MethodPost, "/api/v1/auth/login", map[string]string{"login": login, "password": password})
	require.Equal(b.t, nethttp.StatusOK, status, string(data))
}

// me is the permissions of GET /api/v1/auth/me.
func (b *browser) me() map[string]json.RawMessage {
	b.t.Helper()

	status, data := b.do(nethttp.MethodGet, "/api/v1/auth/me", nil)
	require.Equal(b.t, nethttp.StatusOK, status, string(data))

	var payload struct {
		Access struct {
			Permissions map[string]json.RawMessage `json:"permissions"`
		} `json:"access"`
	}

	require.NoError(b.t, json.Unmarshal(data, &payload))

	return payload.Access.Permissions
}

// Identity and access installed as an application's main does:
//
//	app := gocore.New(cfg, gocore.WithPrefix("/api"))
//	users := identity.Install(app, identity.WithMail(sender), identity.WithCodeSecret(secret),
//		identity.AllowImpersonation("iam.user:impersonate"), identity.WithDeactivationHook(hook))
//	access.Install(app, permissions, users)
//
// An administrator signs in and sees their permissions in /auth/me; somebody
// without a role may not edit roles; after the administrator binds a role to
// them, their /auth/me shows it. The administrator creates a person, who signs
// in, changes their e-mail address with a code from the mail sink, and is
// deactivated once the application's hook lets it. On SQLite, MySQL and MariaDB.
func TestIdentityAndAccessWorkTogether(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("local", db)

		opts := []gocore.Option{gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithPrefix("/api")}
		if db.Name() == "sqlite" {
			opts = append(opts, gocore.WithAutoMigrate(t.Context())) // what gocoretest.New sets
		}

		app := gocore.New(bootstrap.DefaultConfig(), opts...)

		permissions := authz.NewCatalogue()
		permissions.MustDefine("crm.customer:view")
		permissions.MustDefine("iam.user:impersonate", authz.Sensitive())

		sink := mail.NewSink()
		owned := map[string]bool{"dora": true} // the application's rule: dora still owns open records

		users := identity.Install(app,
			identity.WithMail(sink), identity.WithCodeSecret("an-installation-secret-of-32-chars!"),
			identity.AllowImpersonation("iam.user:impersonate"),
			identity.WithDeactivationHook(identity.DeactivationHookFunc(func(_ context.Context, a account.Account, _ int) error {
				if owned[a.Login] {
					return apperr.BadRequest("owns open records").WithReason("crm.owns_records")
				}

				return nil
			})),
		)
		acc := access.Install(app, permissions, users, access.WithRoles(
			authz.Role{Key: "seller", Name: "Seller", Grants: authz.Grants(authz.QualifierAll, "crm.customer:view")},
			authz.ComputedRole("webmaster", "Webmaster", authz.All()),
		))

		if db.Name() != "sqlite" {
			require.NoError(t, app.Migrate(t.Context()), "both modules' migration files")
		}

		for _, a := range []struct {
			id    int
			login string
		}{{1, "admin"}, {2, "ines"}} {
			seed := identitytest.Account(a.id, a.login, "secret")
			seed.CreatedAt = identitytest.Epoch
			_, err := gormstore.New(db).Accounts.Create(t.Context(), seed)
			require.NoError(t, err)
		}

		require.NoError(t, acc.Seed(t.Context(), authz.Subject{Kind: authz.KindUser, ID: 1}, "webmaster"))
		require.NoError(t, app.Check())

		handler, err := app.Handler()
		require.NoError(t, err)

		admin := &browser{t: t, handler: handler, cookies: map[string]*nethttp.Cookie{}}
		ines := &browser{t: t, handler: handler, cookies: map[string]*nethttp.Cookie{}}

		admin.signIn("admin", "secret")
		assert.Contains(t, admin.me(), "iam.role:manage", "the administrator's access is in /auth/me")

		ines.signIn("ines", "secret")
		assert.Empty(t, ines.me(), "nobody gave ines a role yet")

		status, _ := ines.do(nethttp.MethodPost, "/api/v1/iam/roles", accesshttp.CreateRoleInput{
			Name: "Clerk", Grants: []accesshttp.GrantInput{{Permission: "crm.customer:view", Qualifier: "all"}},
		})
		assert.Equal(t, nethttp.StatusForbidden, status, "without a role she may not edit roles")

		status, data := admin.do(nethttp.MethodPost, "/api/v1/iam/users/2/bindings", accesshttp.BindInput{RoleRef: "seller", Level: "organization"})
		require.Equal(t, nethttp.StatusOK, status, string(data))

		assert.Contains(t, ines.me(), "crm.customer:view", "the role the administrator gave shows in her /auth/me")

		// Names come from identity: the SubjectDirectory is *identity.Users itself.
		status, data = admin.do(nethttp.MethodGet, "/api/v1/iam/users/2/access", nil)
		require.Equal(t, nethttp.StatusOK, status, string(data))
		assert.Contains(t, string(data), `"created_by":{"id":1,"name":"admin"}`, "who bound the role is named by identity")

		// Impersonation goes through the engine, with the permission the application named.
		status, _ = ines.do(nethttp.MethodPost, "/api/v1/auth/login-as", map[string]int{"user_id": 1})
		assert.Equal(t, nethttp.StatusForbidden, status)

		// The administrator signs in as ines, the payload says who is really there, and
		// returning needs no password and gives the administrator's own session back.
		status, _ = admin.do(nethttp.MethodPost, "/api/v1/auth/login-as/return", nil)
		assert.Equal(t, nethttp.StatusBadRequest, status, "returning from one's own session is refused")

		status, data = admin.do(nethttp.MethodPost, "/api/v1/auth/login-as", map[string]int{"user_id": 2})
		assert.Equal(t, nethttp.StatusOK, status, string(data))
		assert.Contains(t, string(data), `"impersonator":{"id":1,"name":"admin"}`)
		assert.NotContains(t, admin.me(), "iam.role:manage", "as ines, the administrator has only what ines has")

		asInes := admin.cookies["access_token"]

		status, data = admin.do(nethttp.MethodPost, "/api/v1/auth/login-as/return", nil)
		require.Equal(t, nethttp.StatusOK, status, string(data))
		assert.NotContains(t, string(data), "impersonator")
		assert.Contains(t, admin.me(), "iam.role:manage", "the administrator's permissions are back")

		gone := &browser{t: t, handler: handler, cookies: map[string]*nethttp.Cookie{"access_token": asInes}}
		status, _ = gone.do(nethttp.MethodGet, "/api/v1/auth/me", nil)
		assert.Equal(t, nethttp.StatusUnauthorized, status, "the impersonation session ended with the return")

		// User administration: ines holds no iam.user:manage, the administrator does.

		newUser := identityhttp.CreateUserInput{
			Login: "dora", Name: "Dora Horvat", Email: "dora@old.example", Locale: "hr", Password: "a long password",
		}

		status, _ = ines.do(nethttp.MethodPost, "/api/v1/iam/users", newUser)
		assert.Equal(t, nethttp.StatusForbidden, status, "without iam.user:manage she may not create people")

		status, data = admin.do(nethttp.MethodPost, "/api/v1/iam/users", newUser)
		require.Equal(t, nethttp.StatusOK, status, string(data))

		var dora struct {
			ID int `json:"id"`
		}

		require.NoError(t, json.Unmarshal(data, &dora))

		status, data = admin.do(nethttp.MethodGet, "/api/v1/iam/users?view=all&search=dora", nil)
		require.Equal(t, nethttp.StatusOK, status, string(data))
		assert.Contains(t, string(data), `"total":1`)
		assert.Contains(t, string(data), `"login":"dora"`)

		// dora signs in and changes her e-mail address with the code that was mailed to the new one.
		person := &browser{t: t, handler: handler, cookies: map[string]*nethttp.Cookie{}}
		person.signIn("dora", "a long password")

		status, data = person.do(nethttp.MethodPost, "/api/v1/iam/profile/email", identityhttp.RequestEmailInput{Email: "dora@new.example", CurrentPassword: "a long password"})
		require.Equal(t, nethttp.StatusOK, status, string(data))

		mails := sink.To("dora@new.example")
		require.Len(t, mails, 1, "the code was mailed to the new address, not the old one")
		assert.Empty(t, sink.To("dora@old.example"))

		code := regexp.MustCompile(`\b\d{6}\b`).FindString(mails[0].Text)
		require.NotEmpty(t, code)

		status, data = person.do(nethttp.MethodPost, "/api/v1/iam/profile/email/confirm", identityhttp.ConfirmEmailInput{Code: code})
		require.Equal(t, nethttp.StatusOK, status, string(data))
		assert.Contains(t, string(data), "dora@new.example")

		status, data = admin.do(nethttp.MethodGet, "/api/v1/iam/users/"+strconv.Itoa(dora.ID)+"/changes", nil)
		require.Equal(t, nethttp.StatusOK, status, string(data))
		assert.Contains(t, string(data), `"action":"email"`, "the change is in the audit trail")
		assert.Contains(t, string(data), `"action":"created"`)

		// The application's hook vetoes the deactivation while she owns open records; then it lets it through.
		status, _ = admin.do(nethttp.MethodPost, "/api/v1/iam/users/"+strconv.Itoa(dora.ID)+"/deactivate", nil)
		assert.Equal(t, nethttp.StatusBadRequest, status)

		status, _ = person.do(nethttp.MethodGet, "/api/v1/iam/profile", nil)
		assert.Equal(t, nethttp.StatusOK, status, "a vetoed deactivation leaves her signed in")

		owned["dora"] = false

		status, data = admin.do(nethttp.MethodPost, "/api/v1/iam/users/"+strconv.Itoa(dora.ID)+"/deactivate", nil)
		require.Equal(t, nethttp.StatusNoContent, status, string(data))

		status, _ = person.do(nethttp.MethodGet, "/api/v1/iam/profile", nil)
		assert.Equal(t, nethttp.StatusUnauthorized, status, "deactivating signs her out")

		status, _ = (&browser{t: t, handler: handler, cookies: map[string]*nethttp.Cookie{}}).do(nethttp.MethodPost, "/api/v1/auth/login",
			map[string]string{"login": "dora", "password": "a long password"})
		assert.Equal(t, nethttp.StatusBadRequest, status, "and she cannot sign in again")
	})
}

// A person's activity, as an administrator reads it: what they did, by the areas the
// application named, and what was done while somebody else was signed in as them. On SQLite,
// MySQL and MariaDB.
func TestAPersonsActivity(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
		reg.AddConnection("local", db)

		opts := []gocore.Option{gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithPrefix("/api")}
		if db.Name() == "sqlite" {
			opts = append(opts, gocore.WithAutoMigrate(t.Context()))
		}

		app := gocore.New(bootstrap.DefaultConfig(), opts...)

		permissions := authz.NewCatalogue()
		permissions.MustDefine("iam.user:impersonate", authz.Sensitive())

		users := identity.Install(app, identity.WithoutMail(),
			identity.AllowImpersonation("iam.user:impersonate"),
			identity.WithActivityAreas(identity.Area("people").Types("account"), identity.Area("crm").Types("customers").Prefix("contracts.")),
		)
		acc := access.Install(app, permissions, users, access.WithRoles(authz.ComputedRole("webmaster", "Webmaster", authz.All())))

		if db.Name() != "sqlite" {
			require.NoError(t, app.Migrate(t.Context()))
		}

		for id, login := range []string{1: "admin", 2: "boris", 3: "ines"} {
			if login == "" {
				continue
			}

			_, err := gormstore.New(db).Accounts.Create(t.Context(), identitytest.Account(id, login, "secret"))
			require.NoError(t, err)

			if login != "ines" { // ines is a plain person
				require.NoError(t, acc.Seed(t.Context(), authz.Subject{Kind: authz.KindUser, ID: id}, "webmaster"))
			}
		}

		handler, err := app.Handler()
		require.NoError(t, err)

		admin := &browser{t: t, handler: handler, cookies: map[string]*nethttp.Cookie{}}
		admin.signIn("admin", "secret")

		// The admin's own work: a person made, and a record of the application's, written to the audit trail.
		newUser := identityhttp.CreateUserInput{Login: "dora", Name: "Dora", Email: "dora@example.test", Locale: "en", Password: "a long password"}

		status, data := admin.do(nethttp.MethodPost, "/api/v1/iam/users", newUser)
		require.Equal(t, nethttp.StatusOK, status, string(data))

		trail := audit.NewRepository(database.NewTransactor(db))
		require.NoError(t, trail.Write(t.Context(), audit.NewEntry("contracts.line", 5, 1, "update")))
		require.NoError(t, trail.Write(t.Context(), audit.NewEntry("settings", 9, 1, "delete")))

		rows := func(b *browser, path string) []map[string]any {
			t.Helper()

			status, data := b.do(nethttp.MethodGet, path, nil)
			require.Equal(t, nethttp.StatusOK, status, string(data))

			var page struct {
				Data []map[string]any `json:"data"`
			}

			require.NoError(t, json.Unmarshal(data, &page))

			return page.Data
		}

		all := rows(admin, "/api/v1/iam/users/1/activity")
		require.Len(t, all, 3, "newest first")
		assert.Equal(t, []any{"other", "crm", "people"}, []any{all[0]["area"], all[1]["area"], all[2]["area"]})
		assert.Equal(t, "deleted", all[0]["action"])
		assert.Equal(t, "contracts.line", all[1]["record_type"])
		assert.Equal(t, "created", all[2]["action"])

		_, data = admin.do(nethttp.MethodGet, "/api/v1/iam/users/1/activity?area=crm", nil)
		assert.Contains(t, string(data), `"views":[{"key":"all","count":3},{"key":"people","count":1},{"key":"crm","count":1},{"key":"identity","count":0},{"key":"other","count":1}]`,
			"the counts of every area, whichever is shown")

		assert.Len(t, rows(admin, "/api/v1/iam/users/1/activity?area=crm"), 1)
		assert.Len(t, rows(admin, "/api/v1/iam/users/1/activity?area=other"), 1)
		assert.Len(t, rows(admin, "/api/v1/iam/users/1/activity?from=2000-01-01&to=2000-01-02"), 0)

		// The person who has done nothing has nothing; an area the application did not name is refused.
		assert.Empty(t, rows(admin, "/api/v1/iam/users/2/activity"))

		status, _ = admin.do(nethttp.MethodGet, "/api/v1/iam/users/1/activity?area=vehicles", nil)
		assert.Equal(t, nethttp.StatusUnprocessableEntity, status)

		// Somebody signed in as boris: what is done then is marked with who.
		status, data = admin.do(nethttp.MethodPost, "/api/v1/auth/login-as", map[string]int{"user_id": 2})
		require.Equal(t, nethttp.StatusOK, status, string(data))

		status, data = admin.do(nethttp.MethodPost, "/api/v1/iam/users", identityhttp.CreateUserInput{
			Login: "eva", Name: "Eva", Email: "eva@example.test", Locale: "en", Password: "a long password",
		})
		require.Equal(t, nethttp.StatusOK, status, string(data))

		status, data = admin.do(nethttp.MethodPost, "/api/v1/auth/login-as/return", nil)
		require.Equal(t, nethttp.StatusOK, status, string(data))

		boris := rows(admin, "/api/v1/iam/users/2/activity")
		require.Len(t, boris, 1)
		assert.EqualValues(t, 1, boris[0]["signed_in_as"], "the administrator was signed in as boris")

		for _, row := range rows(admin, "/api/v1/iam/users/1/activity") {
			assert.Nil(t, row["signed_in_as"], "the administrator's own work is not marked")
		}

		// Reading it is for who holds the System permission: a webmaster does, a plain person does not.
		plain := &browser{t: t, handler: handler, cookies: map[string]*nethttp.Cookie{}}
		plain.signIn("ines", "secret")

		status, _ = plain.do(nethttp.MethodGet, "/api/v1/iam/users/1/activity", nil)
		assert.Equal(t, nethttp.StatusForbidden, status)
	})
}
