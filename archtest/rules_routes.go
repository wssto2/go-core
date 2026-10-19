package archtest

import (
	"go/ast"
	"go/token"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	routeImport = "github.com/wssto2/go-core/route"

	// maxConstDepth bounds how many constants one path expression is followed through.
	maxConstDepth = 10
)

var (
	// kebab is a URL path segment: lowercase words joined by "-".
	kebab = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	// digits is a literal numeric segment ("stepper/1").
	digits = regexp.MustCompile(`^[0-9]+$`)
	// word is a single lowercase word, digits allowed after the first letter.
	word = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
)

func isRouteMethod(name string) bool {
	switch name {
	case "Get", "Post", "Put", "Patch", "Delete":
		return true
	}

	return false
}

func isAccessCall(name string) bool {
	switch name {
	case "Requires", "RequiresAny", "Public", "PersonOnly", "Authenticated":
		return true
	}

	return false
}

func isPermissionCall(name string) bool {
	return name == "Requires" || name == "RequiresAny"
}

// declaredRoute is a route.<Method>[In, Out](path) or route.Raw(method, path)
// call and the methods chained after it.
type declaredRoute struct {
	src     Source
	call    *ast.CallExpr
	method  string // GET..DELETE, or the Raw method literal when it is one
	pathArg ast.Expr
	chain   []string
}

// routes finds every route declaration of the production code.
func (p *project) routes() []declaredRoute {
	var out []declaredRoute

	for _, src := range p.files {
		alias := aliasOf(src.File, routeImport)
		if src.IsTest || alias == "" {
			continue
		}

		var stack []ast.Node

		ast.Inspect(src.File, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]

				return true
			}

			stack = append(stack, node)

			if call, isCall := node.(*ast.CallExpr); isCall {
				if method, pathArg, found := routeRoot(call, alias); found {
					out = append(out, declaredRoute{src: src, call: call, method: method, pathArg: pathArg, chain: chainAfter(stack)})
				}
			}

			return true
		})
	}

	return out
}

// routeRoot recognises route.Get[In, Out](path) and route.Raw(method, path).
func routeRoot(call *ast.CallExpr, alias string) (string, ast.Expr, bool) {
	fun := call.Fun

	switch generic := fun.(type) {
	case *ast.IndexExpr:
		return typedRoot(generic.X, call, alias)
	case *ast.IndexListExpr:
		return typedRoot(generic.X, call, alias)
	}

	sel, isSel := fun.(*ast.SelectorExpr)
	if isSel && isIdent(sel.X, alias) && sel.Sel.Name == "Raw" && len(call.Args) == 2 {
		method, _ := stringLit(call.Args[0])

		return method, call.Args[1], true
	}

	return "", nil, false
}

func typedRoot(fun ast.Expr, call *ast.CallExpr, alias string) (string, ast.Expr, bool) {
	sel, isSel := fun.(*ast.SelectorExpr)
	if !isSel || !isIdent(sel.X, alias) || !isRouteMethod(sel.Sel.Name) || len(call.Args) != 1 {
		return "", nil, false
	}

	return strings.ToUpper(sel.Sel.Name), call.Args[0], true
}

// chainAfter lists the methods called on the node at the top of the stack:
// for route.Get[A, B](p).Name("x").Requires("y") it is [Name Requires].
func chainAfter(stack []ast.Node) []string {
	var methods []string

	for idx := len(stack) - 1; idx >= 2; idx -= 2 {
		sel, isSel := stack[idx-1].(*ast.SelectorExpr)
		if !isSel || sel.X != stack[idx] {
			break
		}

		call, isCall := stack[idx-2].(*ast.CallExpr)
		if !isCall || call.Fun != sel {
			break
		}

		methods = append(methods, sel.Sel.Name)
	}

	return methods
}

// packageConsts are the constants of the production files of one directory,
// by name: a route path may use a constant of another file of its package.
func (p *project) packageConsts() map[string]map[string]ast.Expr {
	out := map[string]map[string]ast.Expr{}

	for _, src := range p.files {
		if src.IsTest {
			continue
		}

		dir := path.Dir(src.Rel)
		if out[dir] == nil {
			out[dir] = map[string]ast.Expr{}
		}

		maps.Copy(out[dir], constsOf(src.File))
	}

	return out
}

func constsOf(file *ast.File) map[string]ast.Expr {
	consts := map[string]ast.Expr{}

	for _, decl := range file.Decls {
		gen, isGen := decl.(*ast.GenDecl)
		if !isGen || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			values, isValues := spec.(*ast.ValueSpec)
			if !isValues {
				continue
			}

			for idx, name := range values.Names {
				if idx < len(values.Values) {
					consts[name.Name] = values.Values[idx]
				}
			}
		}
	}

	return consts
}

// evalPath folds a string literal, a constant of the package and "+" of them.
func evalPath(expr ast.Expr, consts map[string]ast.Expr, depth int) (string, bool) {
	if depth > maxConstDepth {
		return "", false
	}

	switch typed := expr.(type) {
	case *ast.BasicLit:
		return stringLit(typed)
	case *ast.ParenExpr:
		return evalPath(typed.X, consts, depth+1)
	case *ast.Ident:
		if value, found := consts[typed.Name]; found {
			return evalPath(value, consts, depth+1)
		}
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return "", false
		}

		left, leftOK := evalPath(typed.X, consts, depth+1)
		right, rightOK := evalPath(typed.Y, consts, depth+1)

		return left + right, leftOK && rightOK
	}

	return "", false
}

func (p *project) routePaths() []finding {
	consts := p.packageConsts()

	var out []finding

	for _, route := range p.routes() {
		where := route.src.At(route.call)

		full, found := evalPath(route.pathArg, consts[path.Dir(route.src.Rel)], 0)
		if !found {
			out = append(out, finding{key: route.src.Rel, text: where +
				"  route path is not a string literal or constants of its package joined with +"})

			continue
		}

		for seg := range strings.SplitSeq(full, "/") {
			if seg != "" && seg[0] != ':' && seg[0] != '*' && !kebab.MatchString(seg) && !digits.MatchString(seg) {
				out = append(out, finding{key: route.src.Rel, text: where +
					"  route path segment " + strconv.Quote(seg) + " in " + strconv.Quote(full)})
			}
		}
	}

	return out
}

// routeAccess: every route declaration chain names who may call it.
func (p *project) routeAccess() []finding {
	consts := p.packageConsts()

	var out []finding

	for _, route := range p.routes() {
		if slices.ContainsFunc(route.chain, isAccessCall) {
			continue
		}

		full, _ := evalPath(route.pathArg, consts[path.Dir(route.src.Rel)], 0)
		if full == "" {
			full = "(path not constant)"
		}

		out = append(out, finding{key: route.src.Rel, text: route.src.At(route.call) + "  " + route.method + " " + full +
			" declares no access: add Requires, RequiresAny, Public, PersonOnly or Authenticated to its chain"})
	}

	return out
}

// routePersonOnly: every route whose full path (PersonOnly.Prefix plus the
// declared path) is in an area carries PersonOnly or Public, and every area is
// the area of some route. A path that is not constant is left to RoutePaths.
func (p *project) routePersonOnly() []finding {
	cfg := p.cfg.PersonOnly
	consts := p.packageConsts()
	hit := map[string]bool{}

	var out []finding

	for _, route := range p.routes() {
		declared, found := evalPath(route.pathArg, consts[path.Dir(route.src.Rel)], 0)
		if !found {
			continue
		}

		full := cfg.Prefix + declared

		areas := areasOf(cfg.Areas, full)
		if len(areas) == 0 {
			continue
		}

		for _, area := range areas {
			hit[area] = true
		}

		if !slices.Contains(route.chain, "PersonOnly") && !slices.Contains(route.chain, "Public") {
			out = append(out, finding{key: route.src.Rel, text: route.src.At(route.call) + "  " + route.method + " " + full +
				" is in a person-only area: declare it .PersonOnly()"})
		}
	}

	return append(out, areasWithoutRoute(cfg, hit)...)
}

func areasWithoutRoute(cfg PersonOnly, hit map[string]bool) []finding {
	var out []finding

	for _, area := range cfg.Areas {
		if !hit[area] && !slices.Contains(cfg.External, area) {
			out = append(out, finding{key: area, text: "person-only area " + strconv.Quote(area) +
				" has no route: fix the area, or list it in PersonOnly.External when a library serves it"})
		}
	}

	return out
}

// areasOf are the areas the path is, or is under.
func areasOf(areas []string, full string) []string {
	return slices.DeleteFunc(slices.Clone(areas), func(area string) bool {
		return full != area && !strings.HasPrefix(full, area+"/")
	})
}

// routeIdentifiers: the names that identify a feature are single lowercase
// words: every segment of the literal prefix of a .Group("...") call (route.Group
// anywhere, any other .Group in the handlers), and every dot-separated namespace
// and the verb of a permission literal passed to Requires or
// RequiresAny. Action sub-paths of a route are not identifiers and are left to
// RoutePaths.
func (p *project) routeIdentifiers() []finding {
	var out []finding

	for _, src := range p.files {
		if src.IsTest {
			continue
		}

		routeAlias := aliasOf(src.File, routeImport)

		ast.Inspect(src.File, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}

			sel, isSel := call.Fun.(*ast.SelectorExpr)
			if !isSel {
				return true
			}

			switch {
			case sel.Sel.Name == "Group" && len(call.Args) > 0 && p.isRouteGroup(src, sel, routeAlias):
				out = append(out, groupIdentifiers(src, call)...)
			case isPermissionCall(sel.Sel.Name):
				out = append(out, permissionIdentifiers(src, call)...)
			}

			return true
		})
	}

	return out
}

// isRouteGroup: a route.Group call, or any .Group (but slog's) in the handlers.
func (p *project) isRouteGroup(src Source, sel *ast.SelectorExpr, routeAlias string) bool {
	if routeAlias != "" && isIdent(sel.X, routeAlias) {
		return true
	}

	handlers := p.cfg.Layers.Handlers

	return handlers != "" && inTree(src.Rel, handlers) && !isIdent(sel.X, "slog")
}

func badWords(src Source, node ast.Node, what, value string, parts []string) []finding {
	var out []finding

	for _, part := range parts {
		if part != "" && part[0] != ':' && !word.MatchString(part) {
			out = append(out, finding{key: src.Rel, text: src.At(node) + "  " + what + " " +
				strconv.Quote(part) + " in " + strconv.Quote(value)})
		}
	}

	return out
}

func groupIdentifiers(src Source, call *ast.CallExpr) []finding {
	value, isLit := stringLit(call.Args[0])
	if !isLit {
		return nil
	}

	return badWords(src, call, "route group segment", value, strings.Split(value, "/"))
}

func permissionIdentifiers(src Source, call *ast.CallExpr) []finding {
	var out []finding

	for _, arg := range call.Args {
		value, isLit := stringLit(arg)
		if !isLit {
			continue
		}

		namespace, verb, hasVerb := strings.Cut(value, ":")
		out = append(out, badWords(src, call, "permission namespace component", value, strings.Split(namespace, "."))...)

		if hasVerb {
			out = append(out, badWords(src, call, "permission verb", value, []string{verb})...)
		}
	}

	return out
}
