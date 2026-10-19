package archtest

import (
	"go/ast"
	"path"
	"slices"
	"strings"
)

const (
	databaseImport = "github.com/wssto2/go-core/database"
	tenancyImport  = "github.com/wssto2/go-core/tenancy"
)

// transactionAPI is go-core's transaction plumbing: a repository reaches the
// caller's transaction through one file of its package, not by calling it.
func transactionAPI() []string {
	return []string{"TxFromContext", "SQLTxFromContext", "NewTransactor", "NewTransactorFromRegistry"}
}

// banned checks Config.Banned. It has no ratchet: a ban is not debt.
func (p *project) banned() []string {
	var out []string

	for _, ban := range p.cfg.Banned {
		found := p.bannedUses(ban)
		if len(found) == 0 {
			continue
		}

		what := ban.Import

		switch {
		case ban.Symbol != "":
			what += "." + ban.Symbol
		case ban.Method != "":
			what += " ." + ban.Method
		case ban.Literal != "":
			what += "." + ban.Literal + "{}"
		}

		out = append(out, report("BANNED ("+what+")", found, ban.Why))
	}

	return out
}

// report is a rule without a ratchet failing: the heading, every finding sorted, and the way out.
func report(heading string, found []finding, why string) string {
	texts := make([]string, len(found))
	for idx, one := range found {
		texts[idx] = one.text
	}

	slices.Sort(texts)

	return heading + ":\n  " + strings.Join(texts, "\n  ") + "\n\n  -> " + why
}

func (p *project) bannedUses(ban Banned) []finding {
	var out []finding

	for _, src := range p.files {
		if !bannedHere(src, ban) {
			continue
		}

		switch {
		case ban.Symbol != "":
			out = append(out, bannedSymbols(src, ban)...)
		case ban.Method != "":
			out = append(out, bannedMethods(src, ban)...)
		case ban.Literal != "":
			out = append(out, bannedLiterals(src, ban)...)
		default:
			out = append(out, p.bannedImports(src, ban)...)
		}
	}

	return out
}

// bannedHere says whether the ban covers the file.
func bannedHere(src Source, ban Banned) bool {
	return placed(src, ban.In, ban.Except, ban.Tests)
}

// placed says whether a rule limited to in (empty: everywhere) and except covers the file.
func placed(src Source, in, except []string, tests bool) bool {
	if src.IsTest && !tests {
		return false
	}

	inPlaceOf := func(place string) bool { return inPlace(place, src.Rel) }

	return (len(in) == 0 || slices.ContainsFunc(in, inPlaceOf)) && !slices.ContainsFunc(except, inPlaceOf)
}

func (p *project) bannedImports(src Source, ban Banned) []finding {
	var out []finding

	for _, imp := range src.File.Imports {
		imported := importPath(imp)
		if imported == "" || !p.importMatches(ban.Import, imported) {
			continue
		}

		if slices.ContainsFunc(ban.Unless, func(pattern string) bool { return p.importMatches(pattern, imported) }) {
			continue
		}

		out = append(out, finding{key: src.Rel, text: src.At(imp) + "  imports " + imported})
	}

	return out
}

func bannedSymbols(src Source, ban Banned) []finding {
	alias := aliasOf(src.File, ban.Import)
	if alias == "" {
		return nil
	}

	var out []finding

	ast.Inspect(src.File, func(node ast.Node) bool {
		sel, isSel := node.(*ast.SelectorExpr)
		if isSel && sel.Sel.Name == ban.Symbol && isIdent(sel.X, alias) {
			out = append(out, finding{key: src.Rel, text: src.At(sel) + "  uses " + alias + "." + ban.Symbol})
		}

		return true
	})

	return out
}

// bannedMethods finds <value>.Method in the files that import ban.Import. Without type information
// the receiver is any expression that is not the name of an imported package, so a package member
// of the same name (migrations.Modules) is not a method.
func bannedMethods(src Source, ban Banned) []finding {
	if aliasOf(src.File, ban.Import) == "" {
		return nil
	}

	packages := map[string]bool{}

	for _, imp := range src.File.Imports {
		if imp.Name != nil {
			packages[imp.Name.Name] = true
		} else {
			packages[lastSegment(importPath(imp))] = true
		}
	}

	var out []finding

	ast.Inspect(src.File, func(node ast.Node) bool {
		sel, isSel := node.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != ban.Method {
			return true
		}

		if ident, isName := sel.X.(*ast.Ident); !isName || !packages[ident.Name] {
			out = append(out, finding{key: src.Rel, text: src.At(sel) + "  uses ." + ban.Method})
		}

		return true
	})

	return out
}

// bannedLiterals finds <alias>.Literal{...} with at least one element in the files that import
// ban.Import; the empty literal is the type's zero value, which sets nothing.
func bannedLiterals(src Source, ban Banned) []finding {
	alias := aliasOf(src.File, ban.Import)
	if alias == "" {
		return nil
	}

	var out []finding

	ast.Inspect(src.File, func(node ast.Node) bool {
		lit, isLit := node.(*ast.CompositeLit)
		if !isLit || len(lit.Elts) == 0 {
			return true
		}

		if sel, isSel := lit.Type.(*ast.SelectorExpr); isSel && sel.Sel.Name == ban.Literal && isIdent(sel.X, alias) {
			out = append(out, finding{key: src.Rel, text: src.At(lit) + "  uses " + alias + "." + ban.Literal + "{...}"})
		}

		return true
	})

	return out
}

// inPlace reports whether rel is in the place: a file ("a/b.go"), a tree
// ("a/..."), or the package directory ("a").
func inPlace(place, rel string) bool {
	switch {
	case strings.HasSuffix(place, ".go"):
		return rel == place
	case strings.HasSuffix(place, "/..."):
		return InTree(rel, strings.TrimSuffix(place, "/..."))
	default:
		return path.Dir(rel) == place
	}
}

// importMatches matches an import path against a Banned pattern.
func (p *project) importMatches(pattern, imported string) bool {
	switch {
	case pattern == "...":
		first, _, _ := strings.Cut(imported, "/")

		return strings.Contains(first, ".") || under(imported, p.module)
	case strings.HasSuffix(pattern, "/..."):
		return under(imported, strings.TrimSuffix(pattern, "/..."))
	default:
		return imported == pattern
	}
}

// repositoryShape: the transaction API of go-core is used in Repository.TxFile
// only and tenancy.ScopeByTenant in Repository.ScopeFile only.
func (p *project) repositoryShape() []finding {
	shape := p.cfg.Repository

	var out []finding

	if shape.TxFile != "" {
		for _, symbol := range transactionAPI() {
			out = append(out, p.bannedUses(restrictedToFile(databaseImport, symbol, shape.Dir, shape.TxFile))...)
		}
	}

	if shape.ScopeFile != "" {
		out = append(out, p.bannedUses(restrictedToFile(tenancyImport, "ScopeByTenant", shape.Dir, shape.ScopeFile))...)
	}

	if shape.NoNestedTransaction {
		var ban Banned

		ban.Import = "gorm.io/gorm"
		ban.Method = "Transaction"
		ban.In = []string{shape.Dir}

		out = append(out, p.bannedUses(ban)...)
	}

	return out
}

// restrictedToFile bans the symbol in every file of the package dir but one.
func restrictedToFile(importPath, symbol, dir, file string) Banned {
	var ban Banned

	ban.Import = importPath
	ban.Symbol = symbol
	ban.In = []string{dir}
	ban.Except = []string{dir + "/" + file}

	return ban
}
