package archtest

import (
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const routeImport = "github.com/wssto2/go-core/route"

var (
	// kebab is a URL path segment: lowercase words joined by "-".
	kebab = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	// digits is a literal numeric segment ("stepper/1").
	digits = regexp.MustCompile(`^[0-9]+$`)
	// word is a single lowercase word, digits allowed after the first letter.
	word = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

	routeMethods = []string{"Get", "Post", "Put", "Patch", "Delete"}
	accessCalls  = []string{"Requires", "RequiresAny", "RequiresAll", "Public", "PersonOnly", "Authenticated"}
)

// declaredRoute is a route.<Method>[In, Out](path) or route.Raw(method, path)
// call and the methods chained after it.
type declaredRoute struct {
	src     source
	call    *ast.CallExpr
	method  string // Get..Delete, or the Raw method literal when it is one
	pathArg ast.Expr
	chain   []string
}

// routes finds every route declaration of the production code.
func (p *project) routes() []declaredRoute {
	var out []declaredRoute

	for _, s := range p.files {
		alias := aliasOf(s.file, routeImport)
		if s.isTest || alias == "" {
			continue
		}

		var stack []ast.Node

		ast.Inspect(s.file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]

				return true
			}

			stack = append(stack, n)

			if call, ok := n.(*ast.CallExpr); ok {
				if r, ok := routeRoot(call, alias); ok {
					r.src = s
					r.chain = chainAfter(stack)
					out = append(out, r)
				}
			}

			return true
		})
	}

	return out
}

// routeRoot recognises route.Get[In, Out](path) and route.Raw(method, path).
func routeRoot(call *ast.CallExpr, alias string) (declaredRoute, bool) {
	fun, typed := call.Fun, false

	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun, typed = f.X, true
	case *ast.IndexListExpr:
		fun, typed = f.X, true
	}

	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return declaredRoute{}, false
	}

	if id, ok := sel.X.(*ast.Ident); !ok || id.Name != alias {
		return declaredRoute{}, false
	}

	switch {
	case typed && slices.Contains(routeMethods, sel.Sel.Name) && len(call.Args) == 1:
		return declaredRoute{call: call, method: strings.ToUpper(sel.Sel.Name), pathArg: call.Args[0]}, true
	case !typed && sel.Sel.Name == "Raw" && len(call.Args) == 2:
		method, _ := stringLit(call.Args[0])

		return declaredRoute{call: call, method: method, pathArg: call.Args[1]}, true
	}

	return declaredRoute{}, false
}

// chainAfter lists the methods called on the node at the top of the stack:
// for route.Get[A, B](p).Name("x").Requires("y") it is [Name Requires].
func chainAfter(stack []ast.Node) []string {
	var methods []string

	for i := len(stack) - 1; i >= 2; i -= 2 {
		sel, ok := stack[i-1].(*ast.SelectorExpr)
		if !ok || sel.X != stack[i] {
			break
		}

		call, ok := stack[i-2].(*ast.CallExpr)
		if !ok || call.Fun != sel {
			break
		}

		methods = append(methods, sel.Sel.Name)
	}

	return methods
}

// packageConsts are the string constants of the production files of one
// directory, by name: a route path may use a const of another file of its package.
func (p *project) packageConsts() map[string]map[string]ast.Expr {
	out := map[string]map[string]ast.Expr{}

	for _, s := range p.files {
		if s.isTest {
			continue
		}

		dir := path.Dir(s.rel)

		for _, d := range s.file.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}

			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}

				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if out[dir] == nil {
							out[dir] = map[string]ast.Expr{}
						}

						out[dir][name.Name] = vs.Values[i]
					}
				}
			}
		}
	}

	return out
}

// evalPath folds a string literal, a constant of the package and "+" of them.
func evalPath(e ast.Expr, consts map[string]ast.Expr, depth int) (string, bool) {
	if depth > 10 {
		return "", false
	}

	switch v := e.(type) {
	case *ast.BasicLit:
		return stringLit(v)
	case *ast.ParenExpr:
		return evalPath(v.X, consts, depth+1)
	case *ast.Ident:
		if c, ok := consts[v.Name]; ok {
			return evalPath(c, consts, depth+1)
		}
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}

		l, lok := evalPath(v.X, consts, depth+1)
		r, rok := evalPath(v.Y, consts, depth+1)

		return l + r, lok && rok
	}

	return "", false
}

func (p *project) routePaths() []finding {
	consts := p.packageConsts()

	var out []finding

	for _, r := range p.routes() {
		at := p.at(r.src, r.call)

		full, ok := evalPath(r.pathArg, consts[path.Dir(r.src.rel)], 0)
		if !ok {
			out = append(out, finding{r.src.rel, at + "  route path is not a string literal or constants of its package joined with +"})

			continue
		}

		for seg := range strings.SplitSeq(full, "/") {
			if seg == "" || seg[0] == ':' || seg[0] == '*' || kebab.MatchString(seg) || digits.MatchString(seg) {
				continue
			}

			out = append(out, finding{r.src.rel, at + "  route path segment " + strconv.Quote(seg) + " in " + strconv.Quote(full)})
		}
	}

	return out
}

// routeAccess: every route declaration chain names who may call it.
func (p *project) routeAccess() []finding {
	consts := p.packageConsts()

	var out []finding

	for _, r := range p.routes() {
		if slices.ContainsFunc(r.chain, func(m string) bool { return slices.Contains(accessCalls, m) }) {
			continue
		}

		full, _ := evalPath(r.pathArg, consts[path.Dir(r.src.rel)], 0)
		if full == "" {
			full = "(path not constant)"
		}

		out = append(out, finding{r.src.rel, p.at(r.src, r.call) + "  " + r.method + " " + full +
			" declares no access: add Requires, RequiresAny, RequiresAll, Public, PersonOnly or Authenticated to its chain"})
	}

	return out
}

// routeIdentifiers: the names that identify a feature are single lowercase
// words: every segment of the literal prefix of a .Group("...") call in the
// handlers, and every dot-separated namespace and the verb of a permission
// literal passed to Requires, RequiresAny or RequiresAll. Action sub-paths of a
// route are not identifiers and are left to RoutePaths.
func (p *project) routeIdentifiers() []finding {
	handlers := p.cfg.Layers.Handlers

	var out []finding

	bad := func(s source, n ast.Node, what, value string, parts []string) {
		for _, part := range parts {
			if part != "" && part[0] != ':' && !word.MatchString(part) {
				out = append(out, finding{s.rel, p.at(s, n) + "  " + what + " " + strconv.Quote(part) + " in " + strconv.Quote(value)})
			}
		}
	}

	for _, s := range p.files {
		if s.isTest {
			continue
		}

		ast.Inspect(s.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			switch {
			case sel.Sel.Name == "Group" && len(call.Args) > 0 && (handlers == "" || inTree(s.rel, handlers)):
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "slog" {
					return true
				}

				if v, ok := stringLit(call.Args[0]); ok {
					bad(s, call, "route group segment", v, strings.Split(v, "/"))
				}
			case slices.Contains([]string{"Requires", "RequiresAny", "RequiresAll"}, sel.Sel.Name):
				for _, arg := range call.Args {
					if v, ok := stringLit(arg); ok {
						ns, verb, hasVerb := strings.Cut(v, ":")
						bad(s, call, "permission namespace component", v, strings.Split(ns, "."))

						if hasVerb {
							bad(s, call, "permission verb", v, []string{verb})
						}
					}
				}
			}

			return true
		})
	}

	return out
}
