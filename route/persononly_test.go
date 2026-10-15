//nolint:paralleltest,exhaustruct,ireturn,varnamelen,nlreturn,noinlineerr,exhaustruct_v5 // tests
package route_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/route"
)

// as authenticates every request as the principal.
func as(p authz.Principal) route.Security {
	return route.Security{Authenticate: []gin.HandlerFunc{func(c *gin.Context) {
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}}}
}

func TestPersonOnlyRefusesAServiceAccount(t *testing.T) {

	routes := []route.Handled{
		route.Get[route.None, string]("/typed").PersonOnly().To(func(_ context.Context, _ route.None) (string, error) { return "ok", nil }),
		route.Raw(http.MethodGet, "/raw").PersonOnly().To(func(c *gin.Context) { c.String(http.StatusOK, "ok") }),
	}

	for name, tc := range map[string]struct {
		who  authz.Principal
		want int
	}{
		"a person":          {authz.User(7, 0), http.StatusOK},
		"a service account": {authz.ServiceAccount(5), http.StatusForbidden},
	} {
		e := newEngine()
		for _, r := range routes {
			if err := r.Mount(e, as(tc.who)); err != nil {
				t.Fatal(err)
			}
		}

		for _, path := range []string{"/typed", "/raw"} {
			rec := do(e, http.MethodGet, path, "")
			if rec.Code != tc.want {
				t.Fatalf("%s %s: status %d, want %d: %s", name, path, rec.Code, tc.want, rec.Body)
			}

			if tc.want == http.StatusForbidden && !strings.Contains(rec.Body.String(), `"why":"person_only"`) {
				t.Fatalf("%s: the refusal does not say why: %s", path, rec.Body)
			}
		}
	}
}

func TestAPublicRouteCannotBePersonOnly(t *testing.T) {
	r := route.Get[route.None, string]("/x").Public().PersonOnly().To(func(_ context.Context, _ route.None) (string, error) { return "", nil })

	if err := r.Mount(newEngine(), open()); err == nil || !strings.Contains(err.Error(), "PersonOnly") {
		t.Fatalf("mount error %v", err)
	}
}
