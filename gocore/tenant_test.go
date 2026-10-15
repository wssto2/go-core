//nolint:testpackage // uses the package's testApp
package gocore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/tenancy"
)

type tenantUser struct{ tenant int }

func (tenantUser) GetID() int         { return 1 }
func (u tenantUser) GetTenantID() int { return u.tenant }
func (u tenantUser) HasTenant() bool  { return u.tenant > 0 }

type plainUser struct{}

func (plainUser) GetID() int { return 2 }

func TestRoutesCarryTheTenantOfTheSignedInPerson(t *testing.T) {
	t.Parallel()

	const (
		tenantSeen, allSeen, noneSeen = "tenant", "all", "none"
		headerWho                     = "X-Who"
	)

	people := map[string]auth.Identifiable{
		tenantSeen: tenantUser{7}, allSeen: tenantUser{tenant: 0}, noneSeen: plainUser{},
	}
	app := testApp(t, "local")
	app.authenticate = []gin.HandlerFunc{func(ctx *gin.Context) {
		auth.SetUser(ctx, people[ctx.GetHeader(headerWho)])
		ctx.Next()
	}}

	probe := route.Get[route.None, string]("/probe").Authenticated()
	app.Routes(probe.To(func(ctx context.Context, _ route.None) (string, error) {
		id, _ := tenancy.TenantIDFromContext(ctx)

		switch {
		case tenancy.AllTenantsFromContext(ctx):
			return allSeen, nil
		case id > 0:
			return tenantSeen, nil
		}

		return noneSeen, nil
	}))

	handler, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	for who := range people {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/probe", nil)
		req.Header.Set(headerWho, who)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if want := `"` + who + `"`; !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: want %s in %s", who, want, rec.Body)
		}
	}
}
