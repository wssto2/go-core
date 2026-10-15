package archtest

import (
	"go/ast"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/wssto2/go-core/validation"
)

// serviceRepositories is Rule 1: a service of Layers.Application/<domain>/...
// injects only Repository interfaces of its own domain (OwnedDomains), of the
// Kernel and of SharedDomains. A foreign domain's data comes through its
// Service. The ratchet key is "<file>|<domain path under Layers.Domain>".
func (p *project) serviceRepositories() []finding {
	app := p.cfg.Layers.Application

	var out []finding

	for _, src := range p.productionIn(app) {
		appDomain, rest, _ := strings.Cut(strings.TrimPrefix(src.Rel, app+"/"), "/")
		if rest == "" {
			continue // a file directly in the application folder: no domain
		}

		out = append(out, p.foreignRepositories(src, p.ownedDomains(appDomain))...)
	}

	return out
}

func (p *project) ownedDomains(appDomain string) []string {
	if owned, found := p.cfg.OwnedDomains[appDomain]; found {
		return owned
	}

	return []string{appDomain}
}

// foreignRepositories lists the domain packages src imports and uses a
// Repository of, which are not owned, shared or the kernel.
func (p *project) foreignRepositories(src Source, owned []string) []finding {
	domain := p.cfg.Layers.Domain
	prefix := p.module + "/" + domain + "/"
	used := repositoryQualifiers(src.File)

	var out []finding

	for _, imp := range src.File.Imports {
		imported := importPath(imp)
		if !strings.HasPrefix(imported, prefix) {
			continue
		}

		domPath := strings.TrimPrefix(imported, prefix)
		top, _, _ := strings.Cut(domPath, "/")

		if slices.Contains(owned, top) || top == p.cfg.Kernel || slices.Contains(p.cfg.SharedDomains, top) {
			continue
		}

		name := lastSegment(imported)
		if imp.Name != nil {
			name = imp.Name.Name
		}

		if used[name] { // a bare import is no injection
			out = append(out, finding{
				key:  src.Rel + "|" + domPath,
				text: src.Rel + " injects a Repository from " + domain + "/" + domPath,
			})
		}
	}

	return out
}

// repositoryQualifiers are the names a file uses as <name>.<Something>Repository.
func repositoryQualifiers(file *ast.File) map[string]bool {
	used := map[string]bool{}

	ast.Inspect(file, func(node ast.Node) bool {
		sel, isSel := node.(*ast.SelectorExpr)
		if isSel && strings.HasSuffix(sel.Sel.Name, "Repository") {
			if qualifier := identName(sel.X); qualifier != "" {
				used[qualifier] = true
			}
		}

		return true
	})

	return used
}

// inputPackage is what boundedStrings needs to know of one package.
type inputPackage struct {
	methods map[string]map[string]bool // type -> method names
	structs map[string]*ast.StructType
	decl    map[string]Source // type -> the file declaring it
}

func (pkg *inputPackage) add(src Source) {
	for _, decl := range src.File.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			pkg.addMethod(typed)
		case *ast.GenDecl:
			pkg.addTypes(src, typed)
		}
	}
}

func (pkg *inputPackage) addMethod(decl *ast.FuncDecl) {
	if decl.Recv == nil || len(decl.Recv.List) != 1 {
		return
	}

	recv := receiverTypeName(decl.Recv.List[0].Type)
	if recv == "" {
		return
	}

	if pkg.methods[recv] == nil {
		pkg.methods[recv] = map[string]bool{}
	}

	pkg.methods[recv][decl.Name.Name] = true
}

func (pkg *inputPackage) addTypes(src Source, gen *ast.GenDecl) {
	for _, spec := range gen.Specs {
		typeSpec, isType := spec.(*ast.TypeSpec)
		if !isType {
			continue
		}

		if structType, isStruct := typeSpec.Type.(*ast.StructType); isStruct {
			pkg.structs[typeSpec.Name.Name] = structType
			pkg.decl[typeSpec.Name.Name] = src
		}
	}
}

// boundedStrings: every string and *string field of an input struct (a type of
// the application with every method of Config.Inputs, and the structs of the
// same package its fields hold) has max: in its validation tag. []string is not
// checked: max: bounds a slice's length, not its elements.
func (p *project) boundedStrings() []finding {
	pkgs := map[string]*inputPackage{}

	for _, src := range p.productionIn(p.cfg.Layers.Application) {
		dir := path.Dir(src.Rel)
		if pkgs[dir] == nil {
			pkgs[dir] = &inputPackage{
				methods: map[string]map[string]bool{},
				structs: map[string]*ast.StructType{},
				decl:    map[string]Source{},
			}
		}

		pkgs[dir].add(src)
	}

	var out []finding

	for _, pkg := range pkgs {
		for name, methods := range pkg.methods {
			_, isStruct := pkg.structs[name]
			isInput := isStruct && !slices.ContainsFunc(p.cfg.Inputs, func(method string) bool { return !methods[method] })

			if isInput {
				pkg.checkBounds(name, map[string]bool{}, &out)
			}
		}
	}

	return out
}

func (pkg *inputPackage) checkBounds(name string, seen map[string]bool, out *[]finding) {
	structType, found := pkg.structs[name]
	if !found || structType.Fields == nil || seen[name] {
		return
	}

	seen[name] = true
	src := pkg.decl[name]

	for _, field := range structType.Fields.List {
		if isStringType(field.Type) {
			if !fieldHasMax(field) {
				*out = append(*out, finding{
					key:  src.Rel,
					text: src.At(field) + "  " + name + "." + fieldNames(field) + " (string) has no max: in its validation tag",
				})
			}

			continue
		}

		if ref := localNamedType(field.Type); ref != "" {
			pkg.checkBounds(ref, seen, out)
		}
	}
}

func fieldNames(field *ast.Field) string {
	if len(field.Names) == 0 {
		return "(embedded)"
	}

	names := make([]string, len(field.Names))
	for idx, ident := range field.Names {
		names[idx] = ident.Name
	}

	return strings.Join(names, ", ")
}

func isStringType(expr ast.Expr) bool {
	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}

	return isIdent(expr, "string")
}

// localNamedType is the identifier of a same-package type held as T, *T, []T
// or []*T; "" for builtins, qualified types and maps.
func localNamedType(expr ast.Expr) string {
	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}

	if arr, isArr := expr.(*ast.ArrayType); isArr {
		expr = arr.Elt
	}

	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}

	name := identName(expr)

	switch name {
	case "string", "bool", "byte", "rune", "error", "any", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128":
		return ""
	}

	return name
}

func tagOf(field *ast.Field, key string) (string, bool) {
	if field.Tag == nil {
		return "", false
	}

	raw, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return "", false
	}

	return reflect.StructTag(raw).Lookup(key)
}

func fieldHasMax(field *ast.Field) bool {
	tag, _ := tagOf(field, "validation")

	for part := range strings.SplitSeq(tag, "|") {
		if strings.HasPrefix(strings.TrimSpace(part), "max:") {
			return true
		}
	}

	return false
}

// validationRules runs every validation tag of the application through
// validation.CheckRules, the check a typed route makes when the app starts.
func (p *project) validationRules() []finding {
	var out []finding

	for _, src := range p.productionIn(p.cfg.Layers.Application) {
		ast.Inspect(src.File, func(node ast.Node) bool {
			if field, isField := node.(*ast.Field); isField {
				out = append(out, unknownRule(src, field)...)
			}

			return true
		})
	}

	return out
}

func unknownRule(src Source, field *ast.Field) []finding {
	tag, has := tagOf(field, "validation")
	if !has {
		return nil
	}

	// The rules do not depend on the field's type: a string field with the same tag is checked in its place.
	var probe reflect.StructField

	probe.Name = "Field"
	probe.Type = reflect.TypeFor[string]()
	probe.Tag = reflect.StructTag(`validation:"` + tag + `"`)

	err := validation.CheckRules(reflect.StructOf([]reflect.StructField{probe}))
	if err == nil {
		return nil
	}

	return []finding{{
		key:  src.Rel,
		text: src.At(field) + "  validation:" + strconv.Quote(tag) + ": " + err.Error() + " (min:0 is the spelling of gte=0)",
	}}
}
