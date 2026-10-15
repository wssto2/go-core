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
		if ban.Symbol != "" {
			what += "." + ban.Symbol
		}

		texts := make([]string, len(found))
		for idx, one := range found {
			texts[idx] = one.text
		}

		slices.Sort(texts)

		out = append(out, "BANNED ("+what+"):\n  "+strings.Join(texts, "\n  ")+"\n\n  -> "+ban.Why)
	}

	return out
}

func (p *project) bannedUses(ban Banned) []finding {
	var out []finding

	for _, src := range p.files {
		if !bannedHere(src, ban) {
			continue
		}

		if ban.Symbol == "" {
			out = append(out, p.bannedImports(src, ban)...)
		} else {
			out = append(out, bannedSymbols(src, ban)...)
		}
	}

	return out
}

// bannedHere says whether the ban covers the file.
func bannedHere(src Source, ban Banned) bool {
	if src.IsTest && !ban.Tests {
		return false
	}

	inPlaceOf := func(place string) bool { return inPlace(place, src.Rel) }

	return (len(ban.In) == 0 || slices.ContainsFunc(ban.In, inPlaceOf)) && !slices.ContainsFunc(ban.Except, inPlaceOf)
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

// inPlace reports whether rel is in the place: a file ("a/b.go"), a tree
// ("a/..."), or the package directory ("a").
func inPlace(place, rel string) bool {
	switch {
	case strings.HasSuffix(place, ".go"):
		return rel == place
	case strings.HasSuffix(place, "/..."):
		return inTree(rel, strings.TrimSuffix(place, "/..."))
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
