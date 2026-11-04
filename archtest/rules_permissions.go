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

// permissionLiteral is the shape of a permission identifier: module[.resource[.section]]:verb.
var permissionLiteral = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){0,2}:[a-z][a-z0-9_]*$`)

// catalogueIndex is what the rule knows of the catalogue: its identifiers and its modules.
type catalogueIndex struct {
	known, modules map[string]bool
}

func newCatalogueIndex(ids []string) catalogueIndex {
	idx := catalogueIndex{known: map[string]bool{}, modules: map[string]bool{}}

	for _, id := range ids {
		idx.known[id] = true
		idx.modules[moduleOf(id)] = true
	}

	return idx
}

// moduleOf is the first segment of an identifier: "crm" of "crm.lead:view".
func moduleOf(id string) string {
	space, _, _ := strings.Cut(id, ":")
	module, _, _ := strings.Cut(space, ".")

	return module
}

func (p *project) permissionCatalogue() []string {
	cat := p.cfg.PermissionCatalogue
	idx := newCatalogueIndex(cat.IDs())
	consts := p.stringConstants()

	var found []finding

	for _, src := range p.files {
		if placed(src, cat.In, cat.Except, cat.Tests) && (cat.Package == "" || !InTree(src.Rel, cat.Package)) {
			found = append(found, p.permissionsIn(src, cat, idx, consts)...)
		}
	}

	if len(found) == 0 {
		return nil
	}

	return []string{report("PERMISSION CATALOGUE", found, orDefault(cat.Why,
		"name only permissions of the catalogue: add the permission there first"))}
}

func (p *project) permissionsIn(src Source, cat PermissionCatalogue, idx catalogueIndex,
	consts map[string]map[string]string,
) []finding {
	imports := p.importDirs(src.File)
	dir := path.Dir(src.Rel)

	var out []finding

	ast.Inspect(src.File, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.BasicLit:
			out = append(out, literalPermission(src, typed, idx)...)
		case *ast.CallExpr:
			if constructorCall(src.File, typed, cat.Constructors) && len(typed.Args) == 2 {
				space, resolved := resolveString(typed.Args[0], dir, imports, consts)
				verb, isVerb := StringLit(typed.Args[1])

				if resolved && isVerb && !idx.known[space+":"+verb] {
					out = append(out, unknownPermission(src, typed, space+":"+verb, " (built by a constructor)"))
				}
			}
		}

		return true
	})

	return out
}

// literalPermission is the finding when lit is shaped like a permission of a known module and is not one.
func literalPermission(src Source, lit *ast.BasicLit, idx catalogueIndex) []finding {
	value, isString := StringLit(lit)
	if !isString || !permissionLiteral.MatchString(value) || !idx.modules[moduleOf(value)] || idx.known[value] {
		return nil
	}

	return []finding{unknownPermission(src, lit, value, "")}
}

func unknownPermission(src Source, node ast.Node, id, how string) finding {
	return finding{
		key:  src.Rel,
		text: src.At(node) + "  " + strconv.Quote(id) + how + " is not in the permission catalogue",
	}
}

// constructorCall reports whether call is one of the constructors ("alias.Func" or "import/path.Func").
func constructorCall(file *ast.File, call *ast.CallExpr, constructors []string) bool {
	return slices.ContainsFunc(constructors, func(spec string) bool {
		dot := strings.LastIndex(spec, ".")
		if dot < 0 {
			return false
		}

		pkg := spec[:dot]
		if strings.Contains(pkg, "/") {
			pkg = aliasOf(file, pkg)
		}

		_, isCall := SelectorCall(call, pkg, spec[dot+1:])

		return isCall && pkg != ""
	})
}

// importDirs maps each name a file gives an import of this module to that package's directory.
func (p *project) importDirs(file *ast.File) map[string]string {
	out := map[string]string{}

	for _, imp := range file.Imports {
		dir, inModule := strings.CutPrefix(importPath(imp), p.module+"/")
		if !inModule {
			continue
		}

		alias := path.Base(dir)
		if imp.Name != nil {
			alias = imp.Name.Name
		}

		out[alias] = dir
	}

	return out
}

// constantDecls are the package-level constants of the module not yet resolved, by directory,
// with the imports of the files that declare them.
type constantDecls struct {
	exprs   map[string]map[string]ast.Expr
	imports map[string]map[string]string
}

// stringConstants indexes the package-level string constants of the module by directory, resolving
// one defined from another package's constant (Namespace = domainlead.Namespace).
func (p *project) stringConstants() map[string]map[string]string {
	decls := constantDecls{exprs: map[string]map[string]ast.Expr{}, imports: map[string]map[string]string{}}
	consts := map[string]map[string]string{}

	for _, src := range p.files {
		if !src.IsTest {
			p.collectConstants(src, decls, consts)
		}
	}

	// resolve until nothing changes: a constant may be defined from another's
	for changed := true; changed; {
		changed = false

		for dir, names := range decls.exprs {
			for name, expr := range names {
				if _, done := consts[dir][name]; done {
					continue
				}

				if value, ok := resolveString(expr, dir, decls.imports[dir], consts); ok {
					consts[dir][name] = value
					changed = true
				}
			}
		}
	}

	return consts
}

func (p *project) collectConstants(src Source, decls constantDecls, consts map[string]map[string]string) {
	dir := path.Dir(src.Rel)

	for _, decl := range src.File.Decls {
		gen, isGen := decl.(*ast.GenDecl)
		if !isGen || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			values, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}

			if decls.exprs[dir] == nil {
				decls.exprs[dir] = map[string]ast.Expr{}
				decls.imports[dir] = map[string]string{}
				consts[dir] = map[string]string{}
			}

			for idx, name := range values.Names {
				if idx < len(values.Values) {
					decls.exprs[dir][name.Name] = values.Values[idx]
				}
			}

			maps.Copy(decls.imports[dir], p.importDirs(src.File))
		}
	}
}

// resolveString evaluates a string literal, a constant of the same package, or pkg.Constant of
// another package of the module.
func resolveString(expr ast.Expr, dir string, imports map[string]string,
	consts map[string]map[string]string,
) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		return StringLit(typed)
	case *ast.Ident:
		value, ok := consts[dir][typed.Name]

		return value, ok
	case *ast.SelectorExpr:
		pkg, isName := typed.X.(*ast.Ident)
		if !isName {
			return "", false
		}

		target, imported := imports[pkg.Name]
		if !imported {
			return "", false
		}

		value, ok := consts[target][typed.Sel.Name]

		return value, ok
	}

	return "", false
}
