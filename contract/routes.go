package contract

import (
	"encoding/json"
	"runtime/debug"
	"sort"
	"strings"
	"unicode"

	"github.com/wssto2/go-core/route"
)

const modulePath = "github.com/wssto2/go-core"

// version is the go-core version that generates, from the build info of the
// program that runs Generate; "(devel)" inside go-core itself.
var version = func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}

	if info.Main.Path == modulePath {
		return info.Main.Version
	}

	for _, dep := range info.Deps {
		if dep.Path == modulePath {
			if dep.Replace != nil && dep.Replace.Version != "" {
				return dep.Replace.Version
			}

			return dep.Version
		}
	}

	return "(unknown)"
}

// routeKey is the property name of a route in its group's table.
func routeKey(group string, spec route.Spec) string {
	if spec.Name != "" {
		name := strings.TrimPrefix(spec.Name, group+".")

		return camel(splitWords(name))
	}

	words := []string{strings.ToLower(spec.Method)}

	for _, seg := range strings.Split(spec.Path, "/") {
		switch {
		case seg == "":
		case seg[0] == ':' || seg[0] == '*':
			words = append(words, "by", seg[1:])
		default:
			words = append(words, splitWords(seg)...)
		}
	}

	return camel(words)
}

func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// camel joins words as lowerCamelCase, keeping the case inside each word.
func camel(words []string) string {
	var b strings.Builder

	for i, w := range words {
		if w == "" {
			continue
		}

		if i == 0 {
			b.WriteString(strings.ToLower(w[:1]) + w[1:])
			continue
		}

		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}

	key := b.String()
	if key != "" && unicode.IsDigit(rune(key[0])) {
		key = "_" + key
	}

	return key
}

func quote(s string) string {
	raw, _ := json.Marshal(s)

	return string(raw)
}

// routesFile renders the group's route table.
func routesFile(group string, p *plan) string {
	var b strings.Builder

	b.WriteString("import { route } from \"@wssto2/vue-core\";\n")

	if names := usedNames(p, func(r routeLine) []string { return r.usesIn }); len(names) > 0 {
		b.WriteString("import type { " + strings.Join(names, ", ") + " } from \"./schemas\";\n")
	}

	if names := usedNames(p, func(r routeLine) []string { return r.usesOut }); len(names) > 0 {
		b.WriteString("import type { " + strings.Join(names, ", ") + " } from \"./entities\";\n")
	}

	b.WriteString("\nexport const " + camel(splitWords(group)) + "Routes = {\n")

	for _, r := range p.routes {
		b.WriteString("  " + r.key + ": " + routeCall(r) + ",\n")
	}

	b.WriteString("} as const;\n")

	return b.String()
}

func usedNames(p *plan, pick func(routeLine) []string) []string {
	set := map[string]bool{}

	for _, r := range p.routes {
		for _, n := range pick(r) {
			set[n] = true
		}
	}

	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}

	sort.Strings(names)

	return names
}

func routeCall(r routeLine) string {
	call := "route"
	if r.spec.Untyped {
		call = "route.raw"
	} else {
		call += "<" + r.in + ", " + r.out + ">"
	}

	args := quote(r.spec.Method) + ", " + quote(r.spec.Path)

	var opts []string
	if r.spec.Public {
		opts = append(opts, "public: true")
	}

	if r.permission != "" {
		opts = append(opts, "permission: "+quote(r.permission))
	}

	if len(opts) > 0 {
		args += ", { " + strings.Join(opts, ", ") + " }"
	}

	return call + "(" + args + ")"
}
