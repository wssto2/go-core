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
var transactionAPI = []string{"TxFromContext", "SQLTxFromContext", "NewTransactor", "NewTransactorFromRegistry"}

// banned checks Config.Banned. It has no ratchet: a ban is not debt.
func (p *project) banned() []string {
	var out []string

	for _, b := range p.cfg.Banned {
		found := p.bannedUses(b)
		if len(found) == 0 {
			continue
		}

		what := b.Import
		if b.Symbol != "" {
			what += "." + b.Symbol
		}

		texts := make([]string, len(found))
		for i, f := range found {
			texts[i] = f.text
		}

		slices.Sort(texts)

		out = append(out, "BANNED ("+what+"):\n  "+strings.Join(texts, "\n  ")+"\n\n  -> "+b.Why)
	}

	return out
}

func (p *project) bannedUses(b Banned) []finding {
	var out []finding

	for _, s := range p.files {
		if s.IsTest && !b.Tests {
			continue
		}

		if len(b.In) > 0 && !slices.ContainsFunc(b.In, func(place string) bool { return inPlace(place, s.Rel) }) {
			continue
		}

		if slices.ContainsFunc(b.Except, func(place string) bool { return inPlace(place, s.Rel) }) {
			continue
		}

		if b.Symbol == "" {
			for _, imp := range s.File.Imports {
				if ip := importPath(imp); ip != "" && p.importMatches(b.Import, ip) && !slices.ContainsFunc(b.Unless, func(u string) bool { return p.importMatches(u, ip) }) {
					out = append(out, finding{s.Rel, p.at(s, imp) + "  imports " + ip})
				}
			}

			continue
		}

		alias := aliasOf(s.File, b.Import)
		if alias == "" {
			continue
		}

		ast.Inspect(s.File, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == b.Symbol {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
					out = append(out, finding{s.Rel, p.at(s, sel) + "  uses " + alias + "." + b.Symbol})
				}
			}

			return true
		})
	}

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
func (p *project) importMatches(pattern, ip string) bool {
	switch {
	case pattern == "...":
		first, _, _ := strings.Cut(ip, "/")

		return strings.Contains(first, ".") || under(ip, p.module)
	case strings.HasSuffix(pattern, "/..."):
		return under(ip, strings.TrimSuffix(pattern, "/..."))
	default:
		return ip == pattern
	}
}

// repositoryShape: the transaction API of go-core is used in Repository.TxFile
// only and tenancy.ScopeByTenant in Repository.ScopeFile only.
func (p *project) repositoryShape() []finding {
	r := p.cfg.Repository

	var out []finding

	if r.TxFile != "" {
		for _, symbol := range transactionAPI {
			out = append(out, p.bannedUses(Banned{
				Import: databaseImport, Symbol: symbol, In: []string{r.Dir}, Except: []string{r.Dir + "/" + r.TxFile},
			})...)
		}
	}

	if r.ScopeFile != "" {
		out = append(out, p.bannedUses(Banned{
			Import: tenancyImport, Symbol: "ScopeByTenant", In: []string{r.Dir}, Except: []string{r.Dir + "/" + r.ScopeFile},
		})...)
	}

	return out
}
