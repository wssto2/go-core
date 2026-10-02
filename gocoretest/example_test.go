package gocoretest_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"testing"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/route"
)

type Ticket struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type ShowInput struct {
	ID int `path:"id"`
}

var Show = route.Get[ShowInput, Ticket]("/tickets/:id")

type Service struct{ app *gocore.App }

func (s Service) Show(_ context.Context, in ShowInput) (Ticket, error) {
	return Ticket{ID: in.ID, Title: s.app.Clock().Now().Format("2006-01-02")}, nil
}

func Install(app *gocore.App) {
	app.Routes(Show.To(Service{app}.Show))
}

// A feature test is plain function calls: build the app, install, call.
func Example() {
	t := &exampleT{}
	defer t.cleanup()

	app := gocoretest.New(t, gocoretest.SignedIn(authz.User(7, 0)))
	Install(app)

	rec := gocoretest.Do(t, app, http.MethodGet, "/tickets/7", nil)
	fmt.Println(gocoretest.Decode[Ticket](t, rec))
	// Output: {7 2026-01-02}
}

// exampleT lets an Example use the helpers that take a testing.TB.
type exampleT struct {
	testing.TB
	cleanups []func()
}

func (t *exampleT) Helper()                      {}
func (t *exampleT) Log(...any)                   {}
func (t *exampleT) Cleanup(f func())             { t.cleanups = append(t.cleanups, f) }
func (t *exampleT) Context() context.Context     { return context.Background() }
func (t *exampleT) Fatal(args ...any)            { log.Fatal(args...) }
func (t *exampleT) Fatalf(f string, args ...any) { log.Fatalf(f, args...) }

func (t *exampleT) cleanup() {
	for _, f := range t.cleanups {
		f()
	}
}
