//nolint:exhaustruct,exhaustruct_v5,varnamelen // tests in the style of the package's others
package savedview_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/savedview"
	"gorm.io/gorm"
)

const (
	catalogue = "vehicles.catalogue"
	ana       = 1
	ivo       = 2
	robot     = 9
	prefix    = "/api"
	views     = prefix + "/v1/saved-views"
)

type fixedClock struct{ at *time.Time }

func (c fixedClock) Now() time.Time { return *c.at }

// world is an application with the module on in-memory SQLite, served under /api.
type world struct {
	t       *testing.T
	db      *gorm.DB
	handler http.Handler
	now     time.Time
}

// newWorld installs the module in an application whose permissions allow everything: the module needs none.
func newWorld(t *testing.T) *world {
	t.Helper()

	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	w := &world{t: t, now: time.Date(2026, time.March, 4, 10, 0, 0, 123456789, time.FixedZone("CET", 3600))}

	app := gocore.New(
		gocore.WithConfig(gocore.DefaultConfig()), gocore.WithRegistry(reg), gocore.WithAutoMigrate(t.Context()), gocore.WithLogger(slog.New(slog.DiscardHandler)),
		gocore.WithAuthentication(signIn), gocore.WithAuthorizer(authztest.AllowAll()), gocore.WithPrefix(prefix),
		gocore.WithClock(fixedClock{at: &w.now}))

	savedview.Install(app)

	w.db = app.Database()

	handler, err := app.Handler()
	require.NoError(t, err)

	w.handler = handler

	return w
}

// signIn signs in the person of the X-User header, or the service account of X-Service.
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

	c.Request = c.Request.WithContext(ctx)
	c.Next()
}

// answer is a response: its status and body.
type answer struct {
	Code int
	Body string
	Data json.RawMessage
}

// as is a request made by the person (or, when negative, the service account; when zero, nobody).
func (w *world) as(who int, method, path string, body any) answer {
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

	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)

	return answer{Code: rec.Code, Body: rec.Body.String(), Data: envelope.Data}
}

//nolint:tagliatelle // snake_case wire
type saveBody struct {
	ListKey string `json:"list_key"`
	Name    string `json:"name"`
	State   string `json:"state"`
}

// save saves a view of the catalogue list as the person.
func (w *world) save(who int, name, state string) answer {
	w.t.Helper()

	return w.as(who, http.MethodPost, views, saveBody{ListKey: catalogue, Name: name, State: state})
}

func (w *world) list(who int, list string) answer {
	w.t.Helper()

	return w.as(who, http.MethodGet, views+"?list="+list, nil)
}
