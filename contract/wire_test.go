package contract_test

import (
	"strings"
	"testing"

	"github.com/wssto2/go-core/contract"
	"github.com/wssto2/go-core/route"
)

const testPkg = "github.com/wssto2/go-core/contract_test"

// Fully tagged: nothing to report, whatever the nesting.
type WireTagged struct {
	ID     int                        `json:"id"`
	Hidden string                     `json:"-"`
	Owner  *WireTaggedOwner           `json:"owner"`
	Tags   []WireTaggedOwner          `json:"tags"`
	ByName map[string]WireTaggedOwner `json:"by_name"`
	WireTaggedBase
	private int
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
	ID    int `json:"id"`
	Title string
	Kept  string                 `json:",omitempty"`
	Child *WireChild             `json:"child"`
	Rows  []WireRow              `json:"rows"`
	Index map[string]WireIndexed `json:"index"`
	WireEmbedded
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

	for _, unwanted := range []string{"WireChild", "private", "Hidden"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%s is tagged or unexported, got:\n%s", unwanted, got)
		}
	}
}

func TestWireFlagsAnOutputFromAForbiddenPackage(t *testing.T) {
	t.Parallel()

	group := route.Group("wire", route.Get[route.None, WireTaggedOwner]("/owner").Name("wire.owner"))

	for name, tc := range map[string]struct {
		rules contract.WireRules
		flag  bool
	}{
		"the package itself":     {contract.WireRules{Forbidden: []string{testPkg}}, true},
		"a parent path":          {contract.WireRules{Forbidden: []string{"github.com/wssto2/go-core"}}, true},
		"an excepted package":    {contract.WireRules{Forbidden: []string{"github.com/wssto2/go-core"}, Except: []string{testPkg}}, false},
		"an excepted parent":     {contract.WireRules{Forbidden: []string{testPkg}, Except: []string{"github.com/wssto2/go-core"}}, false},
		"an unrelated exception": {contract.WireRules{Forbidden: []string{testPkg}, Except: []string{testPkg + "/other"}}, true},
		"a path element prefix":  {contract.WireRules{Forbidden: []string{"github.com/wssto2/go-core/contract"}}, false},
		"another package":        {contract.WireRules{Forbidden: []string{"github.com/other/app/domain"}}, false},
		"no rules":               {contract.WireRules{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			got := wireOf(t, group, tc.rules)
			if flagged := len(got) > 0; flagged != tc.flag {
				t.Errorf("flagged = %v, want %v: %v", flagged, tc.flag, got)
			}

			if tc.flag && !strings.Contains(got[0], "WireTaggedOwner") {
				t.Errorf("the finding does not name the type: %v", got)
			}
		})
	}
}

func TestWireFailsLikeReachable(t *testing.T) {
	t.Parallel()

	bad := route.Group("bad", route.Get[route.None, map[int]string]("/n").Name("bad.n"))

	if _, err := contract.Wire([]*route.Contract{bad}, contract.WireRules{}); err == nil {
		t.Error("want the error Reachable gives for an output with no TypeScript form")
	}
}
