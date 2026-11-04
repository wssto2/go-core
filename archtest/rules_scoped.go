package archtest

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

func (p *project) validateScopedQueries() error {
	for _, one := range p.cfg.ScopedQueries {
		if one.Call == "" || one.Arg == "" || len(one.Via) == 0 || len(one.Files) == 0 {
			return fmt.Errorf("%w: ScopedQueries needs Files, Call, Arg and Via", errConfig)
		}

		for _, place := range one.Files {
			if len(p.scopedFiles(place)) == 0 {
				return fmt.Errorf("%w: ScopedQueries.Files %q holds no Go file (a guard that sees nothing passes forever)",
					errConfig, place)
			}
		}
	}

	return nil
}

func (p *project) scopedFiles(place string) []Source {
	var out []Source

	for _, src := range p.files {
		if !src.IsTest && inPlace(place, src.Rel) {
			out = append(out, src)
		}
	}

	return out
}

func (p *project) scopedQueries() []string {
	var out []string

	for _, one := range p.cfg.ScopedQueries {
		var found []finding

		for _, place := range one.Files {
			for _, src := range p.scopedFiles(place) {
				found = append(found, scopedQueriesIn(src, one)...)
			}
		}

		if len(found) > 0 {
			out = append(out, report("SCOPED QUERIES ("+one.Call+"(&"+one.Arg+"{}))", found, one.Why))
		}
	}

	return out
}

func scopedQueriesIn(src Source, one ScopedQueries) []finding {
	var out []finding

	ast.Inspect(src.File, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall || !callsModel(call, one) {
			return true
		}

		sel, _ := call.Fun.(*ast.SelectorExpr)
		if root := chainRoot(sel.X); !slices.Contains(one.Via, root) {
			from := root
			if from == "" {
				from = "something that is not a call on the receiver"
			}

			out = append(out, finding{key: src.Rel, text: src.At(call) + "  " + one.Call + "(&" + one.Arg + "{}) starts from " +
				from + ", not " + strings.Join(one.Via, " or ")})
		}

		return true
	})

	return out
}

// callsModel reports whether call is <x>.Call(&Arg{}).
func callsModel(call *ast.CallExpr, one ScopedQueries) bool {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != one.Call || len(call.Args) != 1 {
		return false
	}

	unary, isUnary := call.Args[0].(*ast.UnaryExpr)
	if !isUnary || unary.Op != token.AND {
		return false
	}

	lit, isLit := unary.X.(*ast.CompositeLit)

	return isLit && typeName(lit.Type) == one.Arg
}

// typeName spells a type expression: Name or pkg.Name.
func typeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		if pkg, isPkg := typed.X.(*ast.Ident); isPkg {
			return pkg.Name + "." + typed.Sel.Name
		}
	}

	return ""
}

// chainRoot walks a fluent call chain (r.scopedDB(ctx).Where(...).Order(...)) down to the call that
// starts it and returns that call's method or function name; "" when the chain starts from anything else.
func chainRoot(expr ast.Expr) string {
	for {
		call, isCall := expr.(*ast.CallExpr)
		if !isCall {
			return ""
		}

		switch fun := call.Fun.(type) {
		case *ast.Ident:
			return fun.Name
		case *ast.SelectorExpr:
			if _, deeper := fun.X.(*ast.CallExpr); !deeper {
				return fun.Sel.Name
			}

			expr = fun.X
		default:
			return ""
		}
	}
}
