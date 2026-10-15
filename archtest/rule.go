package archtest

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// finding is one violation. key is what the ratchet freezes: the file, or
// "file|domain/path" for ServiceRepositories.
type finding struct {
	key  string
	text string
}

// rule is one check. check returns the problems to report, already settled
// against the ratchet.
type rule struct {
	name  string
	check func() []string
}

// ratcheted turns a finder into a rule: its findings are settled against
// Config.Allowed[name]. A finder that returns nil because its layer is absent
// is switched off by the caller, not here.
func (p *project) ratcheted(name, title, hint string, find func() []finding) rule {
	return rule{name: name, check: func() []string {
		return settle(title, hint, p.cfg.Allowed[name], find())
	}}
}

// settle is the ratchet: a finding whose key is not allowed is a violation,
// and an allowed key with no finding is stale.
func settle(title, hint string, allowed []string, found []finding) []string {
	hits := map[string][]string{}
	for _, f := range found {
		hits[f.key] = append(hits[f.key], f.text)
	}

	var fresh, stale []string

	for key, texts := range hits {
		if !slices.Contains(allowed, key) {
			fresh = append(fresh, texts...)
		}
	}

	for _, key := range allowed {
		if _, ok := hits[key]; !ok {
			stale = append(stale, key)
		}
	}

	slices.Sort(fresh)
	slices.Sort(stale)

	var out []string

	if len(fresh) > 0 {
		out = append(out, "CONVENTION VIOLATION ("+title+"):\n  "+strings.Join(fresh, "\n  ")+"\n\n  -> "+hint+"\n"+
			"  Fix it, or - only with a decision recorded in your architecture document -\n"+
			"  add the entry to this rule's Config.Allowed list.")
	}

	if len(stale) > 0 {
		out = append(out, "STALE ALLOWED ENTRY ("+title+"):\n  "+strings.Join(stale, "\n  ")+"\n\n"+
			"  -> remove these from the rule's Config.Allowed list so the ratchet keeps tightening.")
	}

	return out
}

// inTree reports whether rel is dir or under it.
func inTree(rel, dir string) bool {
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

func inAnyTree(rel string, dirs ...string) bool {
	return slices.ContainsFunc(dirs, func(d string) bool { return d != "" && inTree(rel, d) })
}

// importPath is the unquoted path of an import spec.
func importPath(imp *ast.ImportSpec) string {
	p, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return ""
	}

	return p
}

// under reports whether path is prefix or below it.
func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// selectorCall reports whether n is a call of <pkg>.<fn>(...). pkg is the
// name the file uses; callers that must resolve it pass the alias.
func selectorCall(n ast.Node, pkg, fn string) (*ast.CallExpr, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil, false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}

	id, ok := sel.X.(*ast.Ident)

	return call, ok && id.Name == pkg && sel.Sel.Name == fn
}

// stringLit is the value of a string literal expression.
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}

	v, err := strconv.Unquote(lit.Value)

	return v, err == nil
}

// aliasOf is the name file imports importPath under ("" when it does not).
func aliasOf(file *ast.File, importPath string) string {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err != nil || p != importPath {
			continue
		}

		switch {
		case imp.Name != nil:
			return imp.Name.Name
		default:
			return lastSegment(importPath)
		}
	}

	return ""
}

// lastSegment is the package name an import path implies: its last element,
// without a /vN major-version suffix.
func lastSegment(p string) string {
	parts := strings.Split(p, "/")

	last := parts[len(parts)-1]
	if len(parts) > 1 && len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
		return parts[len(parts)-2]
	}

	return last
}

// productionIn lists the non-test sources under any of the dirs.
func (p *project) productionIn(dirs ...string) []Source {
	var out []Source

	for _, s := range p.files {
		if !s.IsTest && inAnyTree(s.Rel, dirs...) {
			out = append(out, s)
		}
	}

	return out
}

// allIn lists every Source, test files included, under any of the dirs.
func (p *project) allIn(dirs ...string) []Source {
	var out []Source

	for _, s := range p.files {
		if inAnyTree(s.Rel, dirs...) {
			out = append(out, s)
		}
	}

	return out
}

// importsMatching finds, in the sources, imports for which match is true.
func (p *project) importsMatching(srcs []Source, match func(path string) bool) []finding {
	var out []finding

	for _, s := range srcs {
		for _, imp := range s.File.Imports {
			if ip := importPath(imp); ip != "" && match(ip) {
				out = append(out, finding{s.Rel, p.at(s, imp) + "  imports " + ip})
			}
		}
	}

	return out
}

// callsOf finds calls of <pkg>.<fn> where pkg is the import path's name in the file.
func (p *project) callsOf(srcs []Source, importPath, fn string) []finding {
	var out []finding

	for _, s := range srcs {
		alias := aliasOf(s.File, importPath)
		if alias == "" {
			continue
		}

		ast.Inspect(s.File, func(n ast.Node) bool {
			if call, ok := selectorCall(n, alias, fn); ok {
				out = append(out, finding{s.Rel, p.at(s, call)})
			}

			return true
		})
	}

	return out
}
