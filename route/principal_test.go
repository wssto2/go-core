package route_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/middlewares"
	"github.com/wssto2/go-core/route"
)

type appUser struct{ ID, Dealer int }

func (u appUser) GetID() int { return u.ID }

type fixedProvider struct{}

func (fixedProvider) Verify(context.Context, string) (auth.Identifiable, error) {
	return appUser{ID: 7, Dealer: 3}, nil
}

func TestHandlerSeesTheAppsUserAndTheRequestID(t *testing.T) {
	var (
		user appUser
		id   string
	)

	r := route.Get[route.None, string]("/x").To(func(ctx context.Context, _ route.None) (string, error) {
		user, _ = auth.UserOf[appUser](ctx)
		id = route.ExchangeOf(ctx).RequestID()

		return "ok", nil
	})

	e := newEngine()
	e.Use(middlewares.RequestID())

	if err := r.Mount(e, route.Security{Authenticate: []gin.HandlerFunc{auth.Authenticated(fixedProvider{})}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer t")
	req.Header.Set("X-Request-ID", "req-42")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || user != (appUser{7, 3}) || id != "req-42" {
		t.Fatalf("status %d user %+v id %q", rec.Code, user, id)
	}
}

func TestRequestIDFallsBackToTheHeaderWithoutTheMiddleware(t *testing.T) {
	var id string

	r := route.Get[route.None, string]("/x").Public().To(func(ctx context.Context, _ route.None) (string, error) {
		id = route.ExchangeOf(ctx).RequestID()
		return "ok", nil
	})

	e := newEngine()
	_ = r.Mount(e, open())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", "from-header")
	e.ServeHTTP(httptest.NewRecorder(), req)

	if id != "from-header" {
		t.Fatalf("got %q", id)
	}
}
