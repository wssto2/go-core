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
	l := p.cfg.Layers
	prefix := p.module + "/" + l.Domain + "/"

	var out []finding

	for _, s := range p.productionIn(l.Application) {
		appDomain, rest, _ := strings.Cut(strings.TrimPrefix(s.rel, l.Application+"/"), "/")
		if rest == "" {
			continue // a file directly in the application folder: no domain
		}

		owned := []string{appDomain}
		if o, ok := p.cfg.OwnedDomains[appDomain]; ok {
			owned = o
		}

		// Names a file uses as <name>.<Something>Repository.
		used := map[string]bool{}

		ast.Inspect(s.file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && strings.HasSuffix(sel.Sel.Name, "Repository") {
				if id, ok := sel.X.(*ast.Ident); ok {
					used[id.Name] = true
				}
			}

			return true
		})

		for _, imp := range s.file.Imports {
			ip := importPath(imp)
			if !strings.HasPrefix(ip, prefix) {
				continue
			}

			domPath := strings.TrimPrefix(ip, prefix)
			top, _, _ := strings.Cut(domPath, "/")

			if slices.Contains(owned, top) || top == p.cfg.Kernel || slices.Contains(p.cfg.SharedDomains, top) {
				continue
			}

			alias := lastSegment(ip)
			if imp.Name != nil {
				alias = imp.Name.Name
			}

			if used[alias] { // a bare import is no injection
				out = append(out, finding{s.rel + "|" + domPath, s.rel + " injects a Repository from " + l.Domain + "/" + domPath})
			}
		}
	}

	return out
}

// boundedStrings: every string and *string field of an input struct (a type of
// the application with every method of Config.Inputs, and the structs of the
// same package its fields hold) has max: in its validation tag. []string is not
// checked: max: bounds a slice's length, not its elements.
func (p *project) boundedStrings() []finding {
	type pkg struct {
		methods map[string]map[string]bool
		structs map[string]*ast.StructType
		decl    map[string]source
	}

	pkgs := map[string]*pkg{}

	for _, s := range p.productionIn(p.cfg.Layers.Application) {
		dir := path.Dir(s.rel)

		pk := pkgs[dir]
		if pk == nil {
			pk = &pkg{map[string]map[string]bool{}, map[string]*ast.StructType{}, map[string]source{}}
			pkgs[dir] = pk
		}

		for _, decl := range s.file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil && len(d.Recv.List) == 1 {
					if recv := receiverTypeName(d.Recv.List[0].Type); recv != "" {
						if pk.methods[recv] == nil {
							pk.methods[recv] = map[string]bool{}
						}

						pk.methods[recv][d.Name.Name] = true
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						if st, ok := ts.Type.(*ast.StructType); ok {
							pk.structs[ts.Name.Name] = st
							pk.decl[ts.Name.Name] = s
						}
					}
				}
			}
		}
	}

	var out []finding

	for _, pk := range pkgs {
		for name, methods := range pk.methods {
			_, isStruct := pk.structs[name]
			if !isStruct || slices.ContainsFunc(p.cfg.Inputs, func(m string) bool { return !methods[m] }) {
				continue // not a struct, or it lacks one of the input methods
			}

			p.checkBounds(pk.structs, pk.decl, name, map[string]bool{}, &out)
		}
	}

	return out
}

func (p *project) checkBounds(structs map[string]*ast.StructType, decl map[string]source, name string, seen map[string]bool, out *[]finding) {
	st, ok := structs[name]
	if !ok || st.Fields == nil || seen[name] {
		return
	}

	seen[name] = true
	src := decl[name]

	for _, f := range st.Fields.List {
		if isStringType(f.Type) {
			if !fieldHasMax(f) {
				*out = append(*out, finding{src.rel, p.at(src, f) + "  " + name + "." + fieldNames(f) + " (string) has no max: in its validation tag"})
			}

			continue
		}

		if ref := localNamedType(f.Type); ref != "" {
			p.checkBounds(structs, decl, ref, seen, out)
		}
	}
}

func fieldNames(f *ast.Field) string {
	if len(f.Names) == 0 {
		return "(embedded)"
	}

	names := make([]string, len(f.Names))
	for i, n := range f.Names {
		names[i] = n.Name
	}

	return strings.Join(names, ", ")
}

func isStringType(e ast.Expr) bool {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}

	id, ok := e.(*ast.Ident)

	return ok && id.Name == "string"
}

// localNamedType is the identifier of a same-package type held as T, *T, []T
// or []*T; "" for builtins, qualified types and maps.
func localNamedType(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}

	if arr, ok := e.(*ast.ArrayType); ok {
		e = arr.Elt
	}

	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}

	id, ok := e.(*ast.Ident)
	if !ok {
		return ""
	}

	switch id.Name {
	case "string", "bool", "byte", "rune", "error", "any", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128":
		return ""
	}

	return id.Name
}

func tagOf(f *ast.Field, key string) (string, bool) {
	if f.Tag == nil {
		return "", false
	}

	raw, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return "", false
	}

	return reflect.StructTag(raw).Lookup(key)
}

func fieldHasMax(f *ast.Field) bool {
	tag, _ := tagOf(f, "validation")
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

	for _, s := range p.productionIn(p.cfg.Layers.Application) {
		ast.Inspect(s.file, func(n ast.Node) bool {
			f, ok := n.(*ast.Field)
			if !ok {
				return true
			}

			tag, has := tagOf(f, "validation")
			if !has {
				return true
			}

			// The rules do not depend on the field's type: a string field with the same tag is checked in its place.
			typ := reflect.StructOf([]reflect.StructField{{Name: "Field", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(`validation:"` + tag + `"`)}})
			if err := validation.CheckRules(typ); err != nil {
				out = append(out, finding{s.rel, p.at(s, f) + "  validation:" + strconv.Quote(tag) + ": " + err.Error() + " (min:0 is the spelling of gte=0)"})
			}

			return true
		})
	}

	return out
}
