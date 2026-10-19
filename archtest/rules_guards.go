package archtest

import (
	"fmt"
	"go/ast"
	"path"
	"slices"
	"strings"
)

// guardRules registers the rules of Config.FieldWrites, RequiresCall and ContextRoots. Like
// Banned they have no ratchet: a guard is not debt.
func (p *project) guardRules(set *ruleSet) {
	for _, one := range []struct {
		name   string
		used   bool
		finish func() []string
	}{
		{fieldWritesRule, len(p.cfg.FieldWrites) > 0, p.fieldWrites},
		{requiresCallRule, len(p.cfg.RequiresCall) > 0, p.requiresCalls},
		{contextRootsRule, len(p.cfg.ContextRoots) > 0, p.contextRoots},
	} {
		if one.used && !slices.Contains(p.cfg.Skip, one.name) {
			set.rules = append(set.rules, rule{name: one.name, check: one.finish})
		}
	}
}

func (p *project) fieldWrites() []string {
	var out []string

	for _, write := range p.cfg.FieldWrites {
		var found []finding

		for _, src := range p.files {
			if placed(src, write.In, write.Except, write.Tests) {
				found = append(found, p.fieldWritesIn(src, write)...)
			}
		}

		if len(found) > 0 {
			out = append(out, report("FIELD WRITES ("+write.Type+"."+orDefault(write.Field, "*")+")", found, write.Why))
		}
	}

	return out
}

func (p *project) fieldWritesIn(src Source, write FieldWrites) []finding {
	var out []finding

	for _, decl := range src.File.Decls {
		name := ""
		if fn, isFunc := decl.(*ast.FuncDecl); isFunc {
			name = fn.Name.Name
		}

		if writeAllowed(write.Only, src.Rel, name) {
			continue
		}

		ast.Inspect(decl, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				for _, lhs := range typed.Lhs {
					out = append(out, assignedField(src, lhs, write)...)
				}
			case *ast.IncDecStmt:
				out = append(out, assignedField(src, typed.X, write)...)
			case *ast.CompositeLit:
				if write.Literal && p.isLiteralOf(src, typed, write) {
					out = append(out, literalWrites(src, typed, write)...)
				}
			}

			return true
		})
	}

	return out
}

// writeAllowed says whether a write in the named function of the file is on the Only list.
func writeAllowed(only []string, rel, fn string) bool {
	return slices.ContainsFunc(only, func(entry string) bool {
		place, allowedFn, hasFn := strings.Cut(entry, ":")

		return inPlace(place, rel) && (!hasFn || allowedFn == fn)
	})
}

// assignedField is the finding when expr is <x>.Type.Field (any field when write.Field is empty).
func assignedField(src Source, expr ast.Expr, write FieldWrites) []finding {
	sel, isSel := expr.(*ast.SelectorExpr)
	if !isSel || (write.Field != "" && sel.Sel.Name != write.Field) {
		return nil
	}

	if group, isGroup := sel.X.(*ast.SelectorExpr); !isGroup || group.Sel.Name != write.Type {
		return nil
	}

	text := src.At(expr) + "  assigns " + write.Type + "." + sel.Sel.Name

	return []finding{{key: src.Rel, text: text}}
}

// isLiteralOf reports whether lit is a literal of write.Type: bare in the type's own package, or
// through the name the file imports that package under.
func (p *project) isLiteralOf(src Source, lit *ast.CompositeLit, write FieldWrites) bool {
	switch typ := lit.Type.(type) {
	case *ast.Ident:
		return typ.Name == write.Type && (write.Package == "" || path.Dir(src.Rel) == write.Package)
	case *ast.SelectorExpr:
		if typ.Sel.Name != write.Type {
			return false
		}

		if write.Package == "" {
			return true
		}

		alias := aliasOf(src.File, p.module+"/"+write.Package)

		return alias != "" && isIdent(typ.X, alias)
	}

	return false
}

func literalWrites(src Source, lit *ast.CompositeLit, write FieldWrites) []finding {
	var out []finding

	for _, elt := range lit.Elts {
		pair, isPair := elt.(*ast.KeyValueExpr)
		if !isPair {
			continue
		}

		if key, isKey := pair.Key.(*ast.Ident); isKey && (write.Field == "" || key.Name == write.Field) {
			text := src.At(pair) + "  sets " + key.Name + " in a " + write.Type + " literal"
			out = append(out, finding{key: src.Rel, text: text})
		}
	}

	return out
}

func (p *project) requiresCalls() []string {
	var out []string

	for _, req := range p.cfg.RequiresCall {
		callPkg, callName := splitMember(req.Call)
		alsoPkg, alsoName := splitMember(req.Also)

		var found []finding

		for _, src := range p.files {
			if !placed(src, req.In, req.Except, req.Tests) {
				continue
			}

			first := firstCall(src, callPkg, callName)
			if first != nil && firstCall(src, alsoPkg, alsoName) == nil {
				found = append(found, finding{key: src.Rel, text: src.At(first) + "  calls " +
					lastSegment(callPkg) + "." + callName + " without " + lastSegment(alsoPkg) + "." + alsoName})
			}
		}

		if len(found) > 0 {
			heading := "REQUIRES CALL (" + lastSegment(callPkg) + "." + callName + " needs " +
				lastSegment(alsoPkg) + "." + alsoName + ")"
			out = append(out, report(heading, found, req.Why))
		}
	}

	return out
}

// splitMember splits "import/path.Member" at the last dot.
func splitMember(call string) (string, string) {
	dot := strings.LastIndex(call, ".")
	if dot < 0 || strings.Contains(call[dot:], "/") {
		return "", call
	}

	return call[:dot], call[dot+1:]
}

// firstCall is the first call of pkgPath.member in the file, nil when it has none.
func firstCall(src Source, pkgPath, member string) ast.Node {
	alias := aliasOf(src.File, pkgPath)
	if alias == "" {
		return nil
	}

	var first ast.Node

	ast.Inspect(src.File, func(node ast.Node) bool {
		if call, isCall := SelectorCall(node, alias, member); isCall && first == nil {
			first = call
		}

		return first == nil
	})

	return first
}

func (p *project) contextRoots() []string {
	var out []string

	for _, roots := range p.cfg.ContextRoots {
		var found []finding

		for _, src := range p.files {
			if placed(src, nil, nil, roots.Tests) && inAnyTree(src.Rel, roots.Dirs...) {
				found = append(found, unwrappedRoots(src, roots.Wrappers)...)
			}
		}

		if len(found) > 0 {
			out = append(out, report("CONTEXT ROOTS ("+strings.Join(roots.Dirs, ", ")+")", found, roots.Why))
		}
	}

	return out
}

// unwrappedRoots finds context.Background() and context.TODO() that are not the first argument of a wrapper.
func unwrappedRoots(src Source, wrappers []string) []finding {
	alias := aliasOf(src.File, "context")
	if alias == "" {
		return nil
	}

	wrapped := map[ast.Node]bool{}

	ast.Inspect(src.File, func(node ast.Node) bool {
		if call, isCall := node.(*ast.CallExpr); isCall && len(call.Args) > 0 && wrapperCall(src, call, wrappers) {
			wrapped[call.Args[0]] = true
		}

		return true
	})

	var out []finding

	ast.Inspect(src.File, func(node ast.Node) bool {
		for _, fn := range []string{"Background", "TODO"} {
			if _, isRoot := SelectorCall(node, alias, fn); isRoot && !wrapped[node] {
				out = append(out, finding{key: src.Rel, text: fmt.Sprintf("%s  context.%s() as a root context", src.At(node), fn)})
			}
		}

		return true
	})

	return out
}

// wrapperCall reports whether call is one of the wrappers.
func wrapperCall(src Source, call *ast.CallExpr, wrappers []string) bool {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return false
	}

	return slices.ContainsFunc(wrappers, func(wrapper string) bool {
		if method, isMethod := strings.CutPrefix(wrapper, "."); isMethod {
			return sel.Sel.Name == method
		}

		pkg, member := splitMember(wrapper)
		alias := aliasOf(src.File, pkg)

		return alias != "" && isIdent(sel.X, alias) && sel.Sel.Name == member
	})
}
