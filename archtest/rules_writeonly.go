package archtest

import (
	"go/ast"
	"slices"
)

func (p *project) writeOnly() []string {
	var out []string

	for _, one := range p.cfg.WriteOnly {
		var found []finding

		for _, src := range p.files {
			if placed(src, one.In, one.Except, one.Tests) && !slices.ContainsFunc(one.Only, func(place string) bool {
				return inPlace(place, src.Rel)
			}) {
				found = append(found, writeOnlyReads(src, one)...)
			}
		}

		if len(found) > 0 {
			out = append(out, report("WRITE ONLY ("+one.Type+")", found, one.Why))
		}
	}

	return out
}

// writeOnlyReads lists the selectors on one of the fields that are not a write nor a package's name.
func writeOnlyReads(src Source, one WriteOnly) []finding {
	imports := map[string]bool{}

	for _, imp := range src.File.Imports {
		if imp.Name != nil {
			imports[imp.Name.Name] = true
		} else {
			imports[lastSegment(importPath(imp))] = true
		}
	}

	// every expression on the left of an assignment, and each one on the path down to its root
	written := map[ast.Expr]bool{}

	ast.Inspect(src.File, func(node ast.Node) bool {
		if assign, isAssign := node.(*ast.AssignStmt); isAssign {
			for _, lhs := range assign.Lhs {
				for expr := lhs; expr != nil; {
					written[expr] = true

					sel, isSel := expr.(*ast.SelectorExpr)
					if !isSel {
						break
					}

					expr = sel.X
				}
			}
		}

		return true
	})

	var out []finding

	ast.Inspect(src.File, func(node ast.Node) bool {
		sel, isSel := node.(*ast.SelectorExpr)
		if !isSel {
			return true
		}

		if pkg, isName := sel.X.(*ast.Ident); isName && imports[pkg.Name] {
			return false
		}

		if slices.Contains(one.Fields, sel.Sel.Name) && !written[sel] {
			out = append(out, finding{key: src.Rel, text: src.At(sel) + "  reads " + sel.Sel.Name})

			return false
		}

		return true
	})

	return out
}
