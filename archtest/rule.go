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

// rule is one check; check returns the problems to report, already settled
// against the ratchet.
type rule struct {
	name  string
	check func() []string
}

// ruleSet collects the rules Config enables.
type ruleSet struct {
	proj  *project
	rules []rule
}

// add registers a ratcheted rule unless its layers are absent or Config skips it.
func (set *ruleSet) add(enabled bool, name, title, hint string, find func() []finding) {
	if !enabled || slices.Contains(set.proj.cfg.Skip, name) {
		return
	}

	allowed := set.proj.cfg.Allowed[name]

	set.rules = append(set.rules, rule{name: name, check: func() []string {
		return settle(title, hint, allowed, find())
	}})
}

// settle is the ratchet: a finding whose key is not allowed is a violation,
// and an allowed key with no finding is stale.
func settle(title, hint string, allowed []string, found []finding) []string {
	hits := map[string][]string{}
	for _, one := range found {
		hits[one.key] = append(hits[one.key], one.text)
	}

	var fresh, stale []string

	for key, texts := range hits {
		if !slices.Contains(allowed, key) {
			fresh = append(fresh, texts...)
		}
	}

	for _, key := range allowed {
		if _, hit := hits[key]; !hit {
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
	return slices.ContainsFunc(dirs, func(dir string) bool { return dir != "" && inTree(rel, dir) })
}

// under reports whether an import path is prefix or below it.
func under(importPath, prefix string) bool {
	return importPath == prefix || strings.HasPrefix(importPath, prefix+"/")
}

// importPath is the unquoted path of an import spec.
func importPath(imp *ast.ImportSpec) string {
	unquoted, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return ""
	}

	return unquoted
}

// stringLit is the value of a string literal expression.
func stringLit(expr ast.Expr) (string, bool) {
	lit, isLit := expr.(*ast.BasicLit)
	if !isLit || lit.Kind != token.STRING {
		return "", false
	}

	value, err := strconv.Unquote(lit.Value)

	return value, err == nil
}

// aliasOf is the name the file imports pkg under ("" when it does not).
func aliasOf(file *ast.File, pkg string) string {
	for _, imp := range file.Imports {
		if importPath(imp) != pkg {
			continue
		}

		if imp.Name != nil {
			return imp.Name.Name
		}

		return lastSegment(pkg)
	}

	return ""
}

// lastSegment is the package name an import path implies: its last element,
// without a /vN major-version suffix.
func lastSegment(importPath string) string {
	parts := strings.Split(importPath, "/")
	last := parts[len(parts)-1]

	if len(parts) > 1 && len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
		return parts[len(parts)-2]
	}

	return last
}

// selectorCall reports whether node is a call of <pkg>.<member>(...).
func selectorCall(node ast.Node, pkg, member string) (*ast.CallExpr, bool) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return nil, false
	}

	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return nil, false
	}

	ident, isIdent := sel.X.(*ast.Ident)

	return call, isIdent && ident.Name == pkg && sel.Sel.Name == member
}

// productionIn lists the non-test sources under any of the dirs.
func (p *project) productionIn(dirs ...string) []Source {
	return p.sourcesIn(false, dirs...)
}

// allIn lists every source, test files included, under any of the dirs.
func (p *project) allIn(dirs ...string) []Source {
	return p.sourcesIn(true, dirs...)
}

func (p *project) sourcesIn(withTests bool, dirs ...string) []Source {
	var out []Source

	for _, src := range p.files {
		if (withTests || !src.IsTest) && inAnyTree(src.Rel, dirs...) {
			out = append(out, src)
		}
	}

	return out
}

// importsMatching finds, in the sources, imports for which match is true.
func importsMatching(srcs []Source, match func(importPath string) bool) []finding {
	var out []finding

	for _, src := range srcs {
		for _, imp := range src.File.Imports {
			if path := importPath(imp); path != "" && match(path) {
				out = append(out, finding{key: src.Rel, text: src.At(imp) + "  imports " + path})
			}
		}
	}

	return out
}

// callsOf finds calls of <pkg>.<member> where pkg is the name the file gives the import path.
func callsOf(srcs []Source, pkgPath, member string) []finding {
	var out []finding

	for _, src := range srcs {
		alias := aliasOf(src.File, pkgPath)
		if alias == "" {
			continue
		}

		ast.Inspect(src.File, func(node ast.Node) bool {
			if call, isCall := selectorCall(node, alias, member); isCall {
				out = append(out, finding{key: src.Rel, text: src.At(call)})
			}

			return true
		})
	}

	return out
}
