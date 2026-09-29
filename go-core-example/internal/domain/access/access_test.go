package access_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go-core-example/internal/domain/access"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authzhttp"
	"github.com/wssto2/go-core/authz/gormstore"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/middlewares"
	"gorm.io/gorm"
)

// Users of the demo: 1 is a webmaster at the organization, 2 (anna) a
// salesperson at North Motors, 3 (mark) a manager at the North Central
// location, 4 nobody.
type user struct{ id int }

func (u user) GetID() int { return u.id }

type app struct {
	router *gin.Engine
	db     *gorm.DB
}

func newApp(t *testing.T) *app {
	t.Helper()
	db, cleanup, err := database.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = cleanup() })
	require.NoError(t, access.Migrate(db))
	require.NoError(t, gormstore.Migrate(db))

	// the same catalogue, hierarchy, roles, places and store the module uses
	cat, err := access.NewCatalogue()
	require.NoError(t, err)
	h, err := access.NewHierarchy()
	require.NoError(t, err)
	store := gormstore.New(db)
	engine, err := authz.NewEngine(authz.Config{Catalogue: cat, Hierarchy: h, Roles: access.Roles(), Store: store, Resolver: access.NewPlaces(db)})
	require.NoError(t, err)

	require.NoError(t, db.Create(&[]access.Dealer{{ID: 1, Name: "North"}, {ID: 2, Name: "South"}}).Error)
	require.NoError(t, db.Create(&[]access.Location{{ID: 10, DealerID: 1}, {ID: 11, DealerID: 1}, {ID: 20, DealerID: 2}}).Error)
	require.NoError(t, db.Create(&[]access.Staff{{UserID: 2, LocationID: 10}, {UserID: 3, LocationID: 10}}).Error)
	anna, mark := 2, 3
	require.NoError(t, db.Create(&[]access.Lead{
		{ID: 1, DealerID: 1, LocationID: 10, OwnerID: &anna, Kind: "used", Title: "anna used"},
		{ID: 2, DealerID: 1, LocationID: 10, OwnerID: &mark, Kind: "new", Title: "mark new"},
		{ID: 3, DealerID: 1, LocationID: 10, Kind: "used", Title: "pool at 10"},
		{ID: 4, DealerID: 1, LocationID: 11, OwnerID: &anna, Kind: "new", Title: "anna new at 11"},
		{ID: 5, DealerID: 1, LocationID: 11, OwnerID: &mark, Kind: "used", Title: "mark used at 11"},
		{ID: 6, DealerID: 2, LocationID: 20, Kind: "new", Title: "south pool"},
		{ID: 7, DealerID: 2, LocationID: 20, OwnerID: &mark, Kind: "used", Title: "south, owned by mark"},
	}).Error)
	ctx := context.Background()
	sys := authz.Subject{Kind: authz.KindUser, ID: 1}
	for _, b := range []authz.Binding{
		{Subject: sys, Role: authz.RoleRef{Key: access.RoleWebmaster}, Scope: h.Root()},
		{Subject: authz.Subject{Kind: authz.KindUser, ID: 2}, Role: authz.RoleRef{Key: access.RoleSalesperson}, Scope: authz.Scope{Level: "dealer", ID: 1}},
		{Subject: authz.Subject{Kind: authz.KindUser, ID: 3}, Role: authz.RoleRef{Key: access.RoleManager}, Scope: authz.Scope{Level: "location", ID: 10}},
	} {
		_, err := store.Bind(ctx, sys, b)
		require.NoError(t, err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middlewares.ErrorHandler(slog.New(slog.DiscardHandler), nil, false))
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) { // stands in for auth.Authenticated
		if id, err := strconv.Atoi(c.GetHeader("X-User")); err == nil {
			auth.SetUser(c, user{id})
		}
	})
	api.Use(authzhttp.Principals(func(ctx context.Context, i auth.Identifiable) (authz.Principal, bool) {
		var staff access.Staff
		_ = db.WithContext(ctx).Take(&staff, i.GetID()).Error
		return authz.User(i.GetID(), staff.LocationID), true
	}))
	api.Use(authzhttp.PinTenant(engine))
	access.RegisterRoutes(api, access.NewService(db, engine), engine)
	return &app{router: r, db: db}
}

func (a *app) call(t *testing.T, method, path, user, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	if body != "" {
		req = httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set("X-User", user)
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func ids(t *testing.T, body []byte) []int {
	t.Helper()
	var resp struct {
		Data []access.Lead `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	out := []int{}
	for _, l := range resp.Data {
		out = append(out, l.ID)
	}
	return out
}

func TestLeadListIsFilteredByWhatTheCallerMaySee(t *testing.T) {
	a := newApp(t)
	tests := []struct {
		name string
		user string
		want []int
	}{
		{"webmaster at the organization sees every lead", "1", []int{1, 2, 3, 4, 5, 6, 7}},
		// anna: Own at North Motors: her leads and the unassigned pool, never another dealer's
		{"salesperson sees her own leads and the pool of her dealer", "2", []int{1, 3, 4}},
		// mark: manager at North Central only: every lead there, nothing at Harbour or South
		{"manager sees every lead of the location he is bound to", "3", []int{1, 2, 3}},
		{"a user with no binding is turned away", "4", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := a.call(t, http.MethodGet, "/api/v1/leads", tt.user, "")
			if tt.want == nil {
				assert.Equal(t, http.StatusForbidden, code)
				return
			}
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, tt.want, ids(t, body))
		})
	}
	code, _ := a.call(t, http.MethodGet, "/api/v1/leads", "", "")
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestSingleLeadChecksUseTheSameRules(t *testing.T) {
	a := newApp(t)
	get := func(user string, id int) int {
		code, _ := a.call(t, http.MethodGet, "/api/v1/leads/"+strconv.Itoa(id), user, "")
		return code
	}
	assert.Equal(t, 200, get("2", 1), "anna's own lead")
	assert.Equal(t, 200, get("2", 3), "the unassigned pool")
	assert.Equal(t, 403, get("2", 2), "someone else's lead")
	assert.Equal(t, 403, get("2", 6), "another dealer's lead")
	assert.Equal(t, 200, get("3", 2), "the manager sees every lead of his location")
	assert.Equal(t, 403, get("3", 4), "but not the other location of the same dealer")
	assert.Equal(t, 200, get("1", 7), "the webmaster crosses dealers")
	assert.Equal(t, 404, get("1", 99))

	code, _ := a.call(t, http.MethodPut, "/api/v1/leads/1", "2", `{"title":"renamed"}`)
	assert.Equal(t, 200, code, "anna edits her own lead")
	code, _ = a.call(t, http.MethodPut, "/api/v1/leads/2", "2", `{"title":"hijacked"}`)
	assert.Equal(t, 403, code, "and not someone else's")
	code, _ = a.call(t, http.MethodPut, "/api/v1/leads/2", "3", `{"title":"managed"}`)
	assert.Equal(t, 200, code)

	var titles []string
	require.NoError(t, a.db.Model(&access.Lead{}).Where("id IN (1, 2)").Order("id").Pluck("title", &titles).Error)
	assert.Equal(t, []string{"renamed", "managed"}, titles)
}

func TestMeAccessTellsTheFrontendWhatTheCallerHolds(t *testing.T) {
	a := newApp(t)
	code, body := a.call(t, http.MethodGet, "/api/v1/me/access", "2", "")
	require.Equal(t, http.StatusOK, code)
	var resp struct {
		Data authz.MyAccess `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	view := resp.Data.Permissions[access.LeadView]
	assert.Equal(t, authz.QualifierOwn, view.Qualifier)
	assert.Equal(t, authz.Scope{Level: "dealer", ID: 1}, view.Scope)
	assert.NotContains(t, resp.Data.Permissions, access.UserManage)
	assert.False(t, resp.Data.Root)
}
