//nolint:exhaustruct,exhaustruct_v5,lll,tagliatelle // planted types and rules, in the style of the package's other tests
package contract_test

import (
	"strings"
	"testing"

	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/route"
)

const (
	modulePath = "github.com/wssto2/go-core"
	testPkg    = modulePath + "/contract_test"
)

// Fully tagged: nothing to report, whatever the nesting.
type WireTagged struct {
	WireTaggedBase

	ID     int                        `json:"id"`
	Hidden string                     `json:"-"`
	Owner  *WireTaggedOwner           `json:"owner"`
	Tags   []WireTaggedOwner          `json:"tags"`
	ByName map[string]WireTaggedOwner `json:"by_name"`
}

type WireTaggedOwner struct {
	Name string `json:"name"`
}

type WireTaggedBase struct {
	CreatedAt string `json:"created_at"`
}

// Types that write themselves are not read field by field.
type WireOwnJSON struct{ Raw string }

func (WireOwnJSON) MarshalJSON() ([]byte, error) { return []byte(`""`), nil }

type WireOwnText struct{ Raw string }

func (WireOwnText) MarshalText() ([]byte, error) { return nil, nil }

type WireCustom struct {
	A WireOwnJSON `json:"a"`
	B WireOwnText `json:"b"`
}

// Untagged exported fields: at the root, nested in a pointer, a slice and a map, in an embedded struct.
type WireUntagged struct {
	WireEmbedded

	ID    int `json:"id"`
	Title string
	Kept  string                 `json:",omitempty"`
	Child *WireChild             `json:"child"`
	Rows  []WireRow              `json:"rows"`
	Index map[string]WireIndexed `json:"index"`
}

type WireChild struct {
	Name string   `json:"name"`
	Deep WireDeep `json:"deep"`
}

type WireDeep struct{ Level int }

type WireRow struct{ Line string }

type WireIndexed struct{ Key string }

type WireEmbedded struct{ FromEmbedded string }

// A type that contains itself ends the walk.
type WireTree struct {
	Name     string     `json:"name"`
	Children []WireTree `json:"children"`
}

func wireOf(t *testing.T, group *route.Contract, rules contract.WireRules) []string {
	t.Helper()

	got, err := contract.Wire([]*route.Contract{group}, rules)
	if err != nil {
		t.Fatal(err)
	}

	return got
}

func TestWireAcceptsTaggedOutputs(t *testing.T) {
	t.Parallel()

	group := route.Group("wire",
		route.Get[route.None, WireTagged]("/tagged").Name("wire.tagged"),
		route.Get[route.None, WireCustom]("/custom").Name("wire.custom"),
		route.Get[route.None, WireTree]("/tree").Name("wire.tree"),
	)

	if got := wireOf(t, group, contract.WireRules{}); len(got) != 0 {
		t.Errorf("want no finding, got %v", got)
	}
}

func TestWireFlagsUntaggedFieldsWhereverTheyAreNested(t *testing.T) {
	t.Parallel()

	group := route.Group("wire", route.Get[route.None, WireUntagged]("/untagged").Name("wire.untagged"))
	got := strings.Join(wireOf(t, group, contract.WireRules{}), "\n")

	for _, want := range []string{
		"WireUntagged has exported fields without a json tag: Title, Kept",
		"WireDeep has exported fields without a json tag: Level",
		"WireRow has exported fields without a json tag: Line",
		"WireIndexed has exported fields without a json tag: Key",
		"WireEmbedded has exported fields without a json tag: FromEmbedded",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want a finding %q, got:\n%s", want, got)
		}
	}

	for _, unwanted := range []string{"WireChild", "Hidden"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%s is tagged or unexported, got:\n%s", unwanted, got)
		}
	}
}

func TestWireFlagsAnOutputFromAForbiddenPackage(t *testing.T) {
	t.Parallel()

	group := route.Group("wire", route.Get[route.None, WireTaggedOwner]("/owner").Name("wire.owner"))

	for name, rule := range map[string]struct {
		rules contract.WireRules
		flag  bool
	}{
		"the package itself":     {contract.WireRules{Forbidden: []string{testPkg}}, true},
		"a parent path":          {contract.WireRules{Forbidden: []string{modulePath}}, true},
		"an excepted package":    {contract.WireRules{Forbidden: []string{modulePath}, Except: []string{testPkg}}, false},
		"an excepted parent":     {contract.WireRules{Forbidden: []string{testPkg}, Except: []string{modulePath}}, false},
		"an unrelated exception": {contract.WireRules{Forbidden: []string{testPkg}, Except: []string{testPkg + "/other"}}, true},
		"a path element prefix":  {contract.WireRules{Forbidden: []string{modulePath + "/contract"}}, false},
		"another package":        {contract.WireRules{Forbidden: []string{"github.com/other/app/domain"}}, false},
		"no rules":               {contract.WireRules{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := wireOf(t, group, rule.rules)
			if flagged := len(got) > 0; flagged != rule.flag {
				t.Errorf("flagged = %v, want %v: %v", flagged, rule.flag, got)
			}

			if rule.flag && !strings.Contains(got[0], "WireTaggedOwner") {
				t.Errorf("the finding does not name the type: %v", got)
			}
		})
	}
}

func TestWireFailsLikeReachable(t *testing.T) {
	t.Parallel()

	bad := route.Group("bad", route.Get[route.None, map[int]string]("/n").Name("bad.n"))

	_, err := contract.Wire([]*route.Contract{bad}, contract.WireRules{})
	if err == nil {
		t.Error("want the error Reachable gives for an output with no TypeScript form")
	}
}
