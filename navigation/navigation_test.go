package navigation_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/navigation"
)

func titles(nodes []navigation.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.I18n)
	}

	return out
}

func group(children ...navigation.Node) navigation.Node {
	return navigation.Node{I18n: "group", Children: children}
}

func TestADropsAGroupWhoseChildrenAreAllGated(t *testing.T) {
	tree := []navigation.Node{group(
		navigation.Node{I18n: "a", Route: "a", Permissions: []string{"a:view"}},
		navigation.Node{I18n: "b", Route: "b", Permissions: []string{"b:view"}},
	)}

	assert.Empty(t, navigation.Filter(tree, navigation.Held("unrelated:view")))
}

func TestAKeepsAGroupWithOneReachableChild(t *testing.T) {
	tree := []navigation.Node{group(
		navigation.Node{I18n: "a", Route: "a", Permissions: []string{"a:view"}},
		navigation.Node{I18n: "b", Route: "b", Permissions: []string{"b:view"}},
	)}

	got := navigation.Filter(tree, navigation.Held("b:view"))
	assert.Equal(t, []string{"group"}, titles(got))
	assert.Equal(t, []string{"b"}, titles(got[0].Children))
}

func TestAKeepsARoutablePageWhoseChildrenAreFilteredOut(t *testing.T) {
	tree := []navigation.Node{{
		I18n: "catalogue", Route: "catalogue.index", Permissions: []string{"cat:view", "cat:manage"},
		Children: []navigation.Node{{I18n: "models", Permissions: []string{"models:view"}}},
	}}

	assert.Equal(t, []string{"catalogue"}, titles(navigation.Filter(tree, navigation.Held("cat:view"))))
	assert.Empty(t, navigation.Filter(tree, navigation.Held("other")))
}

func TestAKeepsAStaticLinkAndPrunesEmptyBranches(t *testing.T) {
	tree := []navigation.Node{
		{I18n: "static", Route: "static"},
		group(navigation.Node{I18n: "inner", Children: []navigation.Node{{I18n: "leaf", Permissions: []string{"x"}}}}),
	}

	assert.Equal(t, []string{"static"}, titles(navigation.Filter(tree, navigation.Held())))
}

func TestAGateOpensANodeButIsNotSent(t *testing.T) {
	tree := []navigation.Node{{I18n: "reminders", Route: "reminders", Gate: []string{"lead:view"}}}

	got := navigation.Filter(tree, navigation.Held("lead:view"))
	assert.Equal(t, []string{"reminders"}, titles(got))

	raw, err := json.Marshal(got)
	assert.NoError(t, err)
	assert.JSONEq(t, `[{"i18n":"reminders","route":"reminders"}]`, string(raw))
}

func TestRequiresNeedsEveryPermissionAndCoversTheChildren(t *testing.T) {
	tree := []navigation.Node{{
		I18n: "ordered", Route: "ordered", Permissions: []string{"ordered:view"}, Requires: []string{"dispatch:view"},
		Children: []navigation.Node{{I18n: "equipment", Permissions: []string{"ordered:view"}}},
	}}

	assert.Empty(t, navigation.Filter(tree, navigation.Held("ordered:view")))
	assert.Equal(t, []string{"ordered"}, titles(navigation.Filter(tree, navigation.Held("ordered:view", "dispatch:view"))))
}

func TestFilterLeavesTheTreeAlone(t *testing.T) {
	tree := []navigation.Node{group(navigation.Node{I18n: "a", Permissions: []string{"a"}}, navigation.Node{I18n: "b", Permissions: []string{"b"}})}

	_ = navigation.Filter(tree, navigation.Held("a"))
	assert.Len(t, tree[0].Children, 2)
}

func ExampleFilter() {
	menu := []navigation.Node{
		{I18n: "nav.tickets", Route: "tickets.index", Permissions: []string{"tickets.ticket:view"}},
		{I18n: "nav.admin", Children: []navigation.Node{
			{I18n: "nav.roles", Route: "roles.index", Permissions: []string{"access.role:view"}},
		}},
	}

	for _, n := range navigation.Filter(menu, navigation.Held("tickets.ticket:view")) {
		fmt.Println(n.I18n)
	}
	// Output: nav.tickets
}

func ExampleHeld() {
	held := navigation.Held("a:view", "b:view")
	fmt.Println(held("a:view"), held("c:view"))
	// Output: true false
}
