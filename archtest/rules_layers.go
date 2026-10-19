package archtest

import (
	"go/ast"
	"slices"
	"strings"
)

const (
	ginImport       = "github.com/gin-gonic/gin"
	datatableImport = "github.com/wssto2/go-core/datatable"
	resourceImport  = "github.com/wssto2/go-core/resource"
)

// layerImports holds the dependency direction: the domain imports none of the
// outer layers, the application none of infrastructure, interfaces or cmd. Test
// files are held to it too.
func (p *project) layerImports() []finding {
	layers := p.cfg.Layers

	outer := func(dirs ...string) func(string) bool {
		return func(path string) bool {
			return slices.ContainsFunc(dirs, func(dir string) bool { return dir != "" && under(path, p.module+"/"+dir) })
		}
	}

	var out []finding

	if layers.Domain != "" {
		out = append(out, importsMatching(p.allIn(layers.Domain),
			outer(layers.Application, layers.Infrastructure, layers.Interfaces, layers.Cmd))...)
	}

	if layers.Application != "" {
		out = append(out, importsMatching(p.allIn(layers.Application),
			outer(layers.Infrastructure, layers.Interfaces, layers.Cmd))...)
	}

	return out
}

func isORM(path string) bool { return strings.HasPrefix(path, "gorm.io/") }

func (p *project) domainPurity() []finding {
	layers := p.cfg.Layers

	out := importsMatching(p.productionIn(layers.Domain), func(path string) bool {
		return under(path, ginImport) || isORM(path) || path == "encoding/json" ||
			under(path, "github.com/goccy/go-json") || under(path, datatableImport) || under(path, resourceImport)
	})

	// datatable and resource are a query-builder concern: banned in the domain's tests too.
	for _, src := range p.allIn(layers.Domain) {
		if src.IsTest {
			out = append(out, importsMatching([]Source{src}, func(path string) bool {
				return under(path, datatableImport) || under(path, resourceImport)
			})...)
		}
	}

	return append(out, p.domainJSONTags()...)
}

// domainJSONTags finds a json: struct tag outside the shared kernel.
func (p *project) domainJSONTags() []finding {
	layers := p.cfg.Layers
	kernel := layers.Domain + "/" + p.cfg.Kernel

	var out []finding

	for _, src := range p.productionIn(layers.Domain) {
		if InTree(src.Rel, kernel) {
			continue
		}

		ast.Inspect(src.File, func(node ast.Node) bool {
			field, isField := node.(*ast.Field)
			if isField && field.Tag != nil && strings.Contains(field.Tag.Value, "json:") {
				out = append(out, finding{key: src.Rel, text: src.At(field) + "  has a json: tag"})
			}

			return true
		})
	}

	return out
}

func (p *project) applicationPurity() []finding {
	return importsMatching(p.productionIn(p.cfg.Layers.Application), func(path string) bool {
		return under(path, ginImport) || isORM(path)
	})
}

// noTimeNow finds every read of time.Now under dir, called or passed as a value
// (limiter{now: time.Now}).
func (p *project) noTimeNow(dir string) func() []finding {
	return func() []finding {
		var out []finding

		for _, src := range p.productionIn(dir) {
			alias := aliasOf(src.File, "time")
			if alias == "" {
				continue
			}

			ast.Inspect(src.File, func(node ast.Node) bool {
				sel, isSel := node.(*ast.SelectorExpr)
				if isSel && sel.Sel.Name == "Now" && isIdent(sel.X, alias) {
					out = append(out, finding{key: src.Rel, text: src.At(sel)})
				}

				return true
			})
		}

		return out
	}
}

// aggregate is the first folder under dir; "" for a file directly in dir.
func aggregate(rel, dir string) string {
	first, rest, found := strings.Cut(strings.TrimPrefix(rel, dir+"/"), "/")
	if !found || rest == "" {
		return ""
	}

	return first
}

func (p *project) crossAggregate() []finding {
	domain := p.cfg.Layers.Domain
	prefix := p.module + "/" + domain + "/"

	var out []finding

	for _, src := range p.productionIn(domain) {
		own := aggregate(src.Rel, domain)
		if own == "" {
			continue
		}

		for _, imp := range src.File.Imports {
			path := importPath(imp)
			other, _, _ := strings.Cut(strings.TrimPrefix(path, prefix), "/")

			if strings.HasPrefix(path, prefix) && other != "" && other != p.cfg.Kernel && other != own {
				out = append(out, finding{key: src.Rel, text: src.At(imp) + "  imports the " + other + " aggregate (" + path + ")"})
			}
		}
	}

	return out
}

// ctxFirst reports whether the first parameter is context.Context.
func ctxFirst(sig *ast.FuncType) bool {
	if sig.Params == nil || len(sig.Params.List) == 0 {
		return false
	}

	sel, isSel := sig.Params.List[0].Type.(*ast.SelectorExpr)

	return isSel && isIdent(sel.X, "context") && sel.Sel.Name == "Context"
}

func (p *project) domainContextFirst() []finding {
	var out []finding

	for _, src := range p.productionIn(p.cfg.Layers.Domain) {
		ast.Inspect(src.File, func(node ast.Node) bool {
			if spec, isSpec := node.(*ast.TypeSpec); isSpec {
				out = append(out, portsWithoutContext(src, spec)...)
			}

			return true
		})
	}

	return out
}

// portsWithoutContext lists the methods of an interface type whose first parameter is not a context.
func portsWithoutContext(src Source, spec *ast.TypeSpec) []finding {
	iface, isIface := spec.Type.(*ast.InterfaceType)
	if !isIface || iface.Methods == nil {
		return nil
	}

	var out []finding

	for _, method := range iface.Methods.List {
		sig, isFunc := method.Type.(*ast.FuncType)
		if !isFunc || len(method.Names) == 0 || ctxFirst(sig) {
			continue // an embedded interface, or fine
		}

		out = append(out, finding{
			key:  src.Rel,
			text: src.At(method) + "  " + spec.Name.Name + "." + method.Names[0].Name + " does not take context.Context first",
		})
	}

	return out
}

func (p *project) serviceContextFirst() []finding {
	var out []finding

	for _, src := range p.productionIn(p.cfg.Layers.Application) {
		for _, decl := range src.File.Decls {
			method, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || method.Recv == nil || len(method.Recv.List) != 1 || !method.Name.IsExported() {
				continue
			}

			recv := receiverTypeName(method.Recv.List[0].Type)
			if strings.HasSuffix(recv, p.cfg.ServiceType) && !ctxFirst(method.Type) {
				out = append(out, finding{key: src.Rel, text: src.At(method) + "  " + recv + "." +
					method.Name.Name + " does not take context.Context first"})
			}
		}
	}

	return out
}

// receiverTypeName is the bare type name of a method receiver, without the
// pointer or the type parameters.
func receiverTypeName(expr ast.Expr) string {
	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}

	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.IndexExpr:
		return identName(typed.X)
	case *ast.IndexListExpr:
		return identName(typed.X)
	}

	return ""
}

func identName(expr ast.Expr) string {
	if ident, isIdent := expr.(*ast.Ident); isIdent {
		return ident.Name
	}

	return ""
}
