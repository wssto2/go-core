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

func (p *project) permissionCatalogue() []string {
	cat := p.cfg.PermissionCatalogue
	known := map[string]bool{}
	modules := map[string]bool{}

	for _, id := range cat.IDs() {
		known[id] = true
		module, _, _ := strings.Cut(id, ":")
		module, _, _ = strings.Cut(module, ".")
		modules[module] = true
	}

	consts := p.stringConstants()

	var found []finding

	for _, src := range p.files {
		if !placed(src, cat.In, cat.Except, cat.Tests) || (cat.Package != "" && InTree(src.Rel, cat.Package)) {
			continue
		}

		imports := p.importDirs(src.File)
		dir := path.Dir(src.Rel)

		ast.Inspect(src.File, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.BasicLit:
				lit, isString := StringLit(typed)
				if !isString || !permissionLiteral.MatchString(lit) {
					return true
				}

				module, _, _ := strings.Cut(strings.SplitN(lit, ":", 2)[0], ".")
				if modules[module] && !known[lit] {
					found = append(found, finding{key: src.Rel, text: src.At(typed) + "  " + strconv.Quote(lit) +
						" is not in the permission catalogue"})
				}
			case *ast.CallExpr:
				if !constructorCall(src.File, typed, cat.Constructors) || len(typed.Args) != 2 {
					return true
				}

				space, resolved := resolveString(typed.Args[0], dir, imports, consts)
				verb, isVerb := StringLit(typed.Args[1])

				if resolved && isVerb && !known[space+":"+verb] {
					found = append(found, finding{key: src.Rel, text: src.At(typed) + "  " + strconv.Quote(space+":"+verb) +
						" (built by a constructor) is not in the permission catalogue"})
				}
			}

			return true
		})
	}

	if len(found) == 0 {
		return nil
	}

	return []string{report("PERMISSION CATALOGUE", found, orDefault(cat.Why,
		"name only permissions of the catalogue: add the permission there first"))}
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

// stringConstants indexes the package-level string constants of the module by directory, resolving
// one defined from another package's constant (Namespace = domainlead.Namespace).
func (p *project) stringConstants() map[string]map[string]string {
	consts := map[string]map[string]string{}
	pending := map[string]map[string]ast.Expr{}
	imports := map[string]map[string]string{}

	for _, src := range p.files {
		if src.IsTest {
			continue
		}

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

				for idx, name := range values.Names {
					if idx >= len(values.Values) {
						continue
					}

					if pending[dir] == nil {
						pending[dir] = map[string]ast.Expr{}
						imports[dir] = map[string]string{}
						consts[dir] = map[string]string{}
					}

					pending[dir][name.Name] = values.Values[idx]

					maps.Copy(imports[dir], p.importDirs(src.File))
				}
			}
		}
	}

	// resolve until nothing changes: a constant may be defined from another's
	for changed := true; changed; {
		changed = false

		for dir, names := range pending {
			for name, expr := range names {
				if _, done := consts[dir][name]; done {
					continue
				}

				if value, ok := resolveString(expr, dir, imports[dir], consts); ok {
					consts[dir][name] = value
					changed = true
				}
			}
		}
	}

	return consts
}

// resolveString evaluates a string literal, a constant of the same package, or pkg.Constant of
// another package of the module.
func resolveString(expr ast.Expr, dir string, imports map[string]string, consts map[string]map[string]string) (string, bool) {
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
