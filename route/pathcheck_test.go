package route_test

import (
	"strings"
	"testing"

	"github.com/wssto2/go-core/route"
)

type showInput struct {
	ID int `path:"id"`
}

type embeddedInput struct {
	showInput
	Page int `query:"page"`
}

type twoInput struct {
	Org  string `path:"org"`
	Slug string `path:"slug"`
}

func TestPathProblemsBothDirections(t *testing.T) {
	for name, c := range map[string]struct {
		spec route.Spec
		want []string // substrings, one per problem; none means the route is fine
	}{
		"matching":          {route.Get[showInput, string]("/v1/users/:id").Spec(), nil},
		"embedded input":    {route.Get[embeddedInput, string]("/v1/users/:id").Spec(), nil},
		"two parameters":    {route.Get[twoInput, string]("/v1/:org/:slug").Spec(), nil},
		"a wildcard":        {route.Get[showInput, string]("/files/*id").Spec(), nil},
		"no parameters":     {route.Get[route.None, string]("/v1/users").Spec(), nil},
		"raw is unchecked":  {route.Raw("GET", "/v1/users/:id").Spec(), nil},
		"path without tag":  {route.Get[twoInput, string]("/v1/users/:id").Spec(), []string{`the path has :id but twoInput has no field tagged path:"id"`, `twoInput has a field tagged path:"org" but the path has no :org`, `path:"slug" but the path has no :slug`}},
		"tag without path":  {route.Get[showInput, string]("/v1/users").Spec(), []string{`showInput has a field tagged path:"id" but the path has no :id`}},
		"none with a param": {route.Get[route.None, string]("/v1/users/:id").Spec(), []string{`the path has :id but the route has no input (route.None)`}},
	} {
		got := c.spec.PathProblems()
		if len(got) != len(c.want) {
			t.Errorf("%s: %d problems, want %d: %v", name, len(got), len(c.want), got)
			continue
		}

		for i, want := range c.want {
			if !strings.Contains(got[i], want) {
				t.Errorf("%s: problem %d is %q, want it to contain %q", name, i, got[i], want)
			}
		}
	}
}

func TestPathProblemNamesTheRoute(t *testing.T) {
	got := route.Get[showInput, string]("/v1/users/:uid").Spec().PathProblems()
	want := `route GET /v1/users/:uid: the path has :uid but showInput has no field tagged path:"uid": add one or fix the path`

	if len(got) == 0 || got[0] != want {
		t.Errorf("got %v, want %q first", got, want)
	}
}
