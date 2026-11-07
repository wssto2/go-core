// Package navigation is the menu tree an application declares and the filter
// that cuts it down to what one person may reach. identity puts the filtered
// tree in the session payload; the client renders it.
//
//	var Menu = []navigation.Node{
//		{I18n: "nav.tickets", Route: "tickets.index", Permissions: []string{"tickets.ticket:view"}},
//		{I18n: "nav.admin", Children: []navigation.Node{
//			{I18n: "nav.roles", Route: "roles.index", Permissions: []string{"access.role:view"}},
//		}},
//	}
//
//	mine := navigation.Filter(Menu, held)
//
// Permissions are the ids of the application's authz catalogue. The tree
// itself is the application's; go-core only filters it.
package navigation

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrInvalid is a menu node that Validate refuses.
var ErrInvalid = errors.New("invalid navigation node")

// Validate checks a menu tree: a node has a Route or an Href, never both, and an Href is a path starting with a single
// "/" or an http:// or https:// URL with a host (no javascript:, data: or protocol-relative //host). The error wraps
// ErrInvalid and names the node by its I18n key.
func Validate(nodes []Node) error {
	for _, node := range nodes {
		if node.Route != "" && node.Href != "" {
			return fmt.Errorf("%w: %q has both route and href", ErrInvalid, node.I18n)
		}

		if node.Href != "" && !validHref(node.Href) {
			return fmt.Errorf("%w: %q has href %q, which is neither a path nor an http(s) URL", ErrInvalid, node.I18n, node.Href)
		}

		err := Validate(node.Children)
		if err != nil {
			return err
		}
	}

	return nil
}

func validHref(href string) bool {
	if strings.HasPrefix(href, "/") {
		return !strings.HasPrefix(href, "//") && !strings.HasPrefix(href, "/\\") && !strings.ContainsAny(href, "\r\n\t")
	}

	u, err := url.Parse(href)

	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Node is one entry of the menu: a destination (Route set) or a group of Children.
type Node struct {
	// I18n is the key of the label; the client translates it.
	I18n string `json:"i18n"`
	// Icon is the icon's name, when there is one.
	Icon string `json:"icon,omitempty"`
	// Route is the destination's token, which the client binds to one of its own routes.
	Route string `json:"route,omitempty"`
	// Href is a destination outside the app: a path ("/admin-legacy/pages") or an http(s):// URL, drawn by the client
	// as an external link. A node has a Route or an Href, never both (Validate).
	Href     string `json:"href,omitempty"`
	Children []Node `json:"children,omitempty"`
	// Permissions gate the node: a person needs at least one to see it. Empty
	// means everybody who is signed in.
	Permissions []string `json:"permissions,omitempty"`
	// Gate gates the node like Permissions, but a role editor does not offer them
	// on this node: it is a page that has no permissions of its own and follows
	// another node's. Never sent to the client.
	Gate []string `json:"-"`
	// Requires lists permissions the person needs on top of Permissions, all of
	// them, for this node and everything under it. Never sent to the client.
	Requires []string `json:"-"`
}

// Filter returns the part of the tree a person may see, given which permissions
// they hold. The input is not changed.
//
//   - A node whose Requires are not all held is dropped with everything under it.
//   - A group with at least one surviving child stays, so the path to the child is reachable.
//   - A node with Permissions or Gate and no surviving child stays only if one of
//     them is held, even when it is a page that has children of its own.
//   - A group left with no children, and no permissions of its own, goes: there is nothing to navigate to.
//   - A leaf with no permissions at all is a static link and always stays.
func Filter(nodes []Node, held func(permission string) bool) []Node {
	var out []Node

	for _, n := range nodes {
		if !holdsAll(n.Requires, held) {
			continue
		}

		hadChildren := len(n.Children) > 0
		n.Children = Filter(n.Children, held)

		switch {
		case len(n.Children) > 0:
			out = append(out, n)
		case len(n.Permissions) > 0 || len(n.Gate) > 0:
			if holdsOne(n.Permissions, held) || holdsOne(n.Gate, held) {
				out = append(out, n)
			}
		case hadChildren:
		default:
			out = append(out, n)
		}
	}

	return out
}

// Held is the predicate Filter takes, over a list of permission ids.
func Held(permissions ...string) func(string) bool {
	set := make(map[string]struct{}, len(permissions))
	for _, p := range permissions {
		set[p] = struct{}{}
	}

	return func(p string) bool { _, ok := set[p]; return ok }
}

func holdsAll(required []string, held func(string) bool) bool {
	for _, p := range required {
		if !held(p) {
			return false
		}
	}

	return true
}

func holdsOne(permissions []string, held func(string) bool) bool {
	for _, p := range permissions {
		if held(p) {
			return true
		}
	}

	return false
}
