//nolint:err113,exhaustruct,exhaustruct_v5,funcorder,gochecknoglobals,gosec,ireturn,lll,noinlineerr,varnamelen // tests in the style of the package's others
package incident_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/incident"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/tenancy"
	"gorm.io/gorm"
)

// The people of the tests. Person 1 is the staff (holds incident:manage), 2 to 4 may view, file and comment, 5 may
// only view, and service account 9 may read.
const (
	staff     = 1
	ana       = 2
	ivo       = 3
	petra     = 4
	reader    = 5
	robot     = 9
	prefix    = "/api"
	incidents = prefix + "/v1/incidents"
)

type directory map[int]account.Account

func (d directory) Find(_ context.Context, ids []int) ([]account.Account, error) {
	var out []account.Account

	for _, id := range ids {
		if a, ok := d[id]; ok {
			out = append(out, a)
		}
	}

	return out, nil
}

func people() directory {
	return directory{
		staff: {ID: staff, Login: "staff", Name: "Sara Staff", Email: "sara@example.com", Active: true},
		ana:   {ID: ana, Login: "ana", Name: "Ana Anic", Email: "ana@example.com", Active: true},
		ivo:   {ID: ivo, Login: "ivo", Name: "", Email: "ivo@example.com", Active: true},
		petra: {ID: petra, Login: "petra", Name: "Petra P", Email: "petra@example.com", Active: true},
	}
}

// rules is who may do what: a permission list per principal.
type rules map[authz.Subject][]string

func (r rules) holds(ctx context.Context, permission string) bool {
	who, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return false
	}

	return slices.Contains(r[who.Subject], permission)
}

func (r rules) Require(ctx context.Context, permission string) error {
	if r.holds(ctx, permission) {
		return nil
	}

	return apperr.Wrap(authz.ErrForbidden, "access denied", apperr.CodePermissionDenied)
}

func (r rules) RequireOn(ctx context.Context, permission string, _ authz.Resource) error {
	return r.Require(ctx, permission)
}

func (r rules) Access(context.Context, string) (authz.AccessSet, error) {
	return authz.AccessSet{}, nil
}

func user(id int) authz.Subject { return authz.Subject{Kind: authz.KindUser, ID: id} }

var (
	member = []string{incident.ViewIncidents, incident.CreateIncidents, incident.UpdateIncidents}
	// permissions is who may do what in the tests.
	permissions = rules{
		user(staff):  {incident.ViewIncidents, incident.CreateIncidents, incident.UpdateIncidents, incident.ManageIncidents},
		user(ana):    member,
		user(ivo):    member,
		user(petra):  member,
		user(reader): {incident.ViewIncidents},
		{Kind: authz.KindServiceAccount, ID: robot}: {incident.ViewIncidents, incident.CreateIncidents},
	}
)

// signIn signs in the person of the X-User header, or the service account of X-Service, in the tenant of X-Tenant.
func signIn(c *gin.Context) {
	ctx := c.Request.Context()

	switch {
	case c.GetHeader("X-User") != "":
		id, _ := strconv.Atoi(c.GetHeader("X-User"))
		ctx = authz.WithPrincipal(ctx, authz.User(id, 0))
	case c.GetHeader("X-Service") != "":
		id, _ := strconv.Atoi(c.GetHeader("X-Service"))
		ctx = authz.WithPrincipal(ctx, authz.ServiceAccount(id))
	default:
		_ = c.Error(apperr.Unauthorized("user not authenticated"))
		c.Abort()

		return
	}

	if tenant, err := strconv.Atoi(c.GetHeader("X-Tenant")); err == nil {
		ctx = tenancy.WithTenantID(ctx, tenant)
	}

	c.Request = c.Request.WithContext(ctx)
	c.Next()
}

// leakIn is a request that carries secrets in its path and body.
type leakIn struct {
	Token    string `path:"token"`
	Password string `json:"password"`
}

type fixedClock struct{ at *time.Time }

func (c fixedClock) Now() time.Time { return *c.at }

// world is an application with the module on in-memory SQLite, served under /api.
type world struct {
	t       *testing.T
	app     *gocore.App
	db      *gorm.DB
	inc     *incident.Incidents
	handler http.Handler
	now     time.Time
}

// newWorld installs the module with the options, beside four routes of its own: ok, missing, boom (a server error) and
// panic.
func newWorld(t *testing.T, opts ...incident.Option) *world {
	t.Helper()

	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	w := &world{t: t, now: time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)}

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg), gocore.WithAutoMigrate(t.Context()), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAuthentication(signIn), gocore.WithAuthorizer(permissions), gocore.WithPrefix(prefix),
		gocore.WithClock(fixedClock{at: &w.now}))

	catalogue := authz.NewCatalogue()
	require.NoError(t, incident.DefinePermissions(catalogue))
	app.Permissions(catalogue)

	w.app, w.db = app, app.Database()

	app.Use(middlewares.RequestID())

	w.inc = incident.Install(app, people(), opts...)

	type none = route.None

	app.Routes(
		route.Get[none, route.Empty]("/v1/test/ok").Authenticated().To(func(context.Context, none) (route.Empty, error) { return route.Empty{}, nil }),
		route.Get[none, route.Empty]("/v1/test/missing").Authenticated().To(func(context.Context, none) (route.Empty, error) {
			return route.Empty{}, apperr.NotFound("no such thing")
		}),
		route.Get[none, route.Empty]("/v1/test/boom").Authenticated().To(func(context.Context, none) (route.Empty, error) {
			return route.Empty{}, errors.New("database exploded for user 42")
		}),
		route.Post[leakIn, route.Empty]("/v1/test/leak/:token").Authenticated().To(func(context.Context, leakIn) (route.Empty, error) {
			return route.Empty{}, apperr.Internal(errors.New("save failed")).WithFields(map[string]string{"password": "hunter2-field"})
		}),
		route.Get[none, route.Empty]("/v1/test/panic").Authenticated().To(func(context.Context, none) (route.Empty, error) {
			var nothing []int

			return route.Empty{}, errors.New(strconv.Itoa(nothing[3]))
		}),
	)

	handler, err := app.Handler()
	require.NoError(t, err)

	w.handler = withRecovery(handler)

	return w
}

// withRecovery puts go-core's panic recovery around the handler, the outermost middleware of a running application:
// Handler() leaves it out. A panic of the inner engine propagates up to it, as it does in production.
func withRecovery(inner http.Handler) http.Handler {
	outer := gin.New()
	outer.Use(middlewares.PanicRecovery(slog.New(slog.DiscardHandler)))
	outer.NoRoute(gin.WrapH(inner))

	return outer
}

// answer is a response: its status and the envelope's data and meta.
type answer struct {
	Code int
	Body []byte
	Data json.RawMessage
	Meta json.RawMessage
}

// as is a request made by the person (or, when negative, the service account), with extra headers.
func (w *world) as(who int, method, path string, body any, headers ...string) answer {
	w.t.Helper()

	var reader *bytes.Reader

	switch typed := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(typed))
	default:
		raw, err := json.Marshal(body)
		require.NoError(w.t, err)

		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequestWithContext(w.t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	switch {
	case who > 0:
		req.Header.Set("X-User", strconv.Itoa(who))
	case who < 0:
		req.Header.Set("X-Service", strconv.Itoa(-who))
	}

	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)

	var envelope struct {
		Data json.RawMessage `json:"data"`
		Meta json.RawMessage `json:"meta"`
	}

	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)

	return answer{Code: rec.Code, Body: rec.Body.Bytes(), Data: envelope.Data, Meta: envelope.Meta}
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()

	var out T

	require.NoError(t, json.Unmarshal(raw, &out), string(raw))

	return out
}

// row is one row of a table, as a map.
func (w *world) rows(table string) []map[string]any {
	w.t.Helper()

	var out []map[string]any

	require.NoError(w.t, w.db.Table(table).Order("id").Find(&out).Error)

	for _, row := range out {
		for key, value := range row {
			if pointer, ok := value.(*any); ok {
				value = *pointer
			}

			if raw, ok := value.([]byte); ok {
				value = string(raw)
			}

			row[key] = value
		}
	}

	return out
}
