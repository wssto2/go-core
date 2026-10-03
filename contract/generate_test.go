package contract

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wssto2/go-core/datatable"
	"github.com/wssto2/go-core/route"
)

var update = flag.Bool("update", false, "rewrite the golden files")

type Person struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Ticket struct {
	ID       int       `json:"id"`
	Title    string    `json:"title"`
	Assignee *Person   `json:"assignee"`
	Tags     []string  `json:"tags"`
	Opened   time.Time `json:"opened"`
}

type Row struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type ShowInput struct {
	ID int `path:"id"`
}

type ListInput struct {
	Status string `query:"status" validation:"max:20"`
	Page   int    `query:"page"`
}

type Draft struct {
	Title string `json:"title" validation:"required|max:100"`
	Owner Person `json:"owner"`
}

type Stats struct {
	Open int `json:"open"`
}

var (
	list   = route.Get[ListInput, []Row]("/tickets")
	paged  = route.Get[ListInput, datatable.DatatableResult[Row]]("/tickets/paged").Name("tickets.paged")
	show   = route.Get[ShowInput, Ticket]("/tickets/:id").Name("tickets.show").Requires("tickets.ticket:view")
	create = route.Post[Draft, Ticket]("/tickets").Name("tickets.create").Requires("tickets.ticket:manage")
	remove = route.Delete[ShowInput, route.Empty]("/tickets/:id")
	send   = route.Post[ShowInput, route.Empty]("/tickets/:id/send-order")
	health = route.Get[route.None, string]("/health").Public()
	events = route.Raw("GET", "/events").Public()
	export = route.Raw("GET", "/export").Requires("tickets.ticket:view")

	tickets = route.Group("tickets", list, paged, show, create, remove, send, health, events, export).Types(Stats{})
)

func fixedVersion(t *testing.T) {
	t.Helper()

	old := version
	version = func() string { return "v0.0.0-test" }

	t.Cleanup(func() { version = old })
}

func TestGenerateMatchesGolden(t *testing.T) {
	fixedVersion(t)

	dir := t.TempDir()
	if err := Generate(dir, tickets); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"entities.ts", "schemas.ts", "routes.ts"} {
		got, err := os.ReadFile(filepath.Join(dir, "tickets", name)) //nolint:gosec // test temp dir
		if err != nil {
			t.Fatal(err)
		}

		golden := filepath.Join("testdata", "tickets", name)
		if *update {
			if err := os.MkdirAll(filepath.Dir(golden), 0o750); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(golden, got, 0o600); err != nil { //nolint:gosec // fixed testdata path
				t.Fatal(err)
			}
		}

		want, err := os.ReadFile(golden) //nolint:gosec // fixed testdata path
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != string(want) {
			t.Errorf("%s differs from %s (run go test ./contract -update):\n%s", name, golden, got)
		}
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	fixedVersion(t)

	var first string

	for i := range 5 {
		dir := t.TempDir()
		if err := Generate(dir, tickets); err != nil {
			t.Fatal(err)
		}

		got := ""
		for _, name := range []string{"entities.ts", "schemas.ts", "routes.ts"} {
			b, _ := os.ReadFile(filepath.Join(dir, "tickets", name)) //nolint:gosec // test temp dir
			got += string(b)
		}

		if i == 0 {
			first = got
		} else if got != first {
			t.Fatal("two runs wrote different files")
		}
	}
}

func TestRouteKeys(t *testing.T) {
	named := []struct{ group, name, want string }{
		{"tickets", "tickets.show", "show"},
		{"identity", "identity.users.show", "usersShow"},
		{"tickets", "ping", "ping"},
		{"tickets", "tickets.send-order", "sendOrder"},
		{"tickets", "other.tickets.create", "otherTicketsCreate"},
	}

	for _, c := range named {
		spec := route.Get[route.None, string]("/x").Name(c.name).Spec()
		if got := routeKey(c.group, spec); got != c.want {
			t.Errorf("%s in %s: %s, want %s", c.name, c.group, got, c.want)
		}
	}

	derived := map[string]string{
		"GET /tickets":                  "getTickets",
		"GET /tickets/:id":              "getTicketsById",
		"POST /tickets/:id/send-order":  "postTicketsByIdSendOrder",
		"DELETE /tickets/:id/events/:e": "deleteTicketsByIdEventsByE",
		"GET /":                         "get",
		"GET /files/*path":              "getFilesByPath",
	}

	for line, want := range derived {
		method, path, _ := strings.Cut(line, " ")
		if got := routeKey("tickets", route.Raw(method, path).Spec()); got != want {
			t.Errorf("%s: %s, want %s", line, got, want)
		}
	}
}

type Page[T any] struct {
	Items []T `json:"items"`
}

type Level int

func TestGenerateRefusesWhatItCannotDo(t *testing.T) {
	generic := route.Group("generic", route.Get[route.None, Page[Row]]("/rows"))
	enum := route.Group("enum", route.Get[route.None, string]("/x")).Types(Level(0))
	badInput := route.Group("badinput", route.Get[int, string]("/x"))
	clash := route.Group("clash", route.Get[ShowInput, string]("/a"), route.Get[route.None, ShowInput]("/b"))
	sameKey := route.Group("samekey", route.Get[route.None, string]("/a"), route.Get[route.None, string]("/a"))
	loose := route.Group("Bad_Name")

	for _, c := range []struct {
		group *route.Contract
		want  string
	}{
		{generic, "non-generic"},
		{enum, "only structs"},
		{badInput, "route.None"},
		{clash, "both the input of a route and an output"},
		{sameKey, "both become"},
		{loose, "lowercase words"},
	} {
		err := Generate(t.TempDir(), c.group)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error containing %q, got %v", c.group.Name(), c.want, err)
		}
	}
}
