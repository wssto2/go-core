package archtest

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

const (
	ginImport      = "github.com/gin-gonic/gin"
	datatableImport = "github.com/wssto2/go-core/datatable"
	resourceImport = "github.com/wssto2/go-core/resource"
)

// rules lists the checks Config enables, in a stable order.
func (p *project) rules() []rule {
	l := p.cfg.Layers
	skip := func(name string) bool { return slices.Contains(p.cfg.Skip, name) }

	var all []rule

	add := func(enabled bool, r rule) {
		if enabled && !skip(r.name) {
			all = append(all, r)
		}
	}

	domain, app := l.Domain != "", l.Application != ""

	add(domain || app, p.ratcheted(NoPanic, "no panic in application/domain",
		"return an error so the caller decides", p.noPanic))
	add(app, p.ratcheted(NoContextBackground, "no context.Background() in the application layer",
		"accept ctx from the caller so cancellation, deadlines and tenancy/auth values propagate", p.noContextBackground))
	add(domain || app, p.ratcheted(NoSleep, "no time.Sleep in application/domain",
		"use a ctx-bound timer or ticker, or move scheduling to the caller", p.noSleep))
	add(domain || app, p.ratcheted(NoDatabaseSQL, "no database/sql outside infrastructure",
		"keep persistence in the infrastructure layer behind domain repository interfaces", p.noDatabaseSQL))
	add(true, p.ratcheted(ErrorfWrap, "fmt.Errorf wrapping err must use %w",
		"use %w so errors.Is/As keep working across the wrap", p.errorfWrap))
	add(l.Handlers != "" && l.Persistence != "" && l.Handlers != l.Persistence,
		p.ratcheted(HandlersNoPersistence, "handlers must not import repository implementations",
			"call an application service; the persistence package is wired in the composition root", p.handlersNoPersistence))
	add(domain || app, p.ratcheted(LayerImports, "layers import inward only",
		"the domain is the innermost layer and the application depends on the domain only: keep the type pure and "+
			"map to the outer-layer representation at the boundary, or depend on a domain interface that the "+
			"composition root implements", p.layerImports))
	add(domain, p.ratcheted(DomainPurity, "the domain is framework-free",
		"no web framework, ORM, database/sql, encoding/json, go-core datatable/resource or json: tag in the domain: "+
			"marshal in the adapter and map at the boundary (the shared kernel may carry json tags)", p.domainPurity))
	add(app, p.ratcheted(ApplicationPurity, "the application layer is framework-free",
		"no web framework, ORM or database/sql in the application layer: keep gin in the handlers and "+
			"*gorm.DB in infrastructure, expose a transaction port from the domain", p.applicationPurity))
	add(domain, p.ratcheted(NoTimeNowDomain, "no time.Now() in the domain",
		"take a Clock dependency and call clock.Now() so tests can control time", p.noTimeNow(l.Domain)))
	add(app, p.ratcheted(NoTimeNowApplication, "no time.Now() in the application",
		"take a Clock in the constructor and call clock.Now()", p.noTimeNow(l.Application)))
	add(domain, p.ratcheted(CrossAggregate, "no cross-aggregate domain imports",
		"reference the other aggregate by ID and compose it in the application layer", p.crossAggregate))
	add(domain, p.ratcheted(DomainContextFirst, "domain interface methods take context.Context as the first parameter",
		"thread ctx from the caller so cancellation, deadlines and tenancy propagate; a genuinely context-free port "+
			"(a clock, a pure policy) goes on this rule's Allowed list with a note in your architecture document", p.domainContextFirst))
	add(app, p.ratcheted(ServiceContextFirst, "exported application service methods take context.Context as the first parameter",
		"accept ctx from the handler and pass it to the repository", p.serviceContextFirst))
	add(app, p.ratcheted(BoundedStrings, "every string input field declares a max: bound",
		"an unbounded string reaches its VARCHAR(n) column unchecked - an over-long value is a 500, not a clean 422. "+
			`Add the bound to the validation tag, sized to the backing column, e.g. validation:"required|max:150"`+
			" - it then propagates to the generated Zod schema", p.boundedStrings))
	add(true, p.ratcheted(RoutePaths, "route paths are kebab-case",
		"every segment of a route.Get/Post/Put/Patch/Delete/Raw path is lowercase words joined by -; :param and *param are allowed", p.routePaths))
	add(true, p.ratcheted(RouteIdentifiers, "route groups and permissions are single lowercase words",
		"name each segment of a route group, each dot-separated namespace of a permission and its verb as one lowercase word "+
			"with no separators; action sub-paths on a route are exempt", p.routeIdentifiers))
	add(app, p.ratcheted(ValidationRules, "validation rules are known",
		"a validation tag that names a rule go-core's validator does not know (gte=0 for min:0, a typo) fails every request "+
			"of that input with 500 and no hint", p.validationRules))
	add(true, p.ratcheted(RouteAccess, "every route declares who may call it",
		"end the declaration chain with Requires, RequiresAny, RequiresAll, Public, PersonOnly or Authenticated; "+
			"an undeclared route is open to every signed-in caller", p.routeAccess))
	add(domain && app, p.ratcheted(ServiceRepositories, "services inject only their own aggregate's repositories",
		"obtain that domain's data through its Service, not its Repository", p.serviceRepositories))
	add(p.cfg.Repository.Dir != "" && (p.cfg.Repository.TxFile != "" || p.cfg.Repository.ScopeFile != ""),
		p.ratcheted(RepositoryShape, "repository shape",
			"a repository reaches the caller's transaction and tenant scope through the one file of the package that owns them", p.repositoryShape))

	if len(p.cfg.Banned) > 0 {
		all = append(all, rule{name: "banned", check: p.banned})
	}

	return all
}

func (p *project) appAndDomain() []source {
	return p.productionIn(p.cfg.Layers.Application, p.cfg.Layers.Domain)
}

func (p *project) noPanic() []finding {
	var out []finding

	for _, s := range p.appAndDomain() {
		ast.Inspect(s.file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
					out = append(out, finding{s.rel, p.at(s, call) + "  panic("})
				}
			}

			return true
		})
	}

	return out
}

func (p *project) noContextBackground() []finding {
	return p.callsOf(p.productionIn(p.cfg.Layers.Application), "context", "Background")
}

func (p *project) noSleep() []finding { return p.callsOf(p.appAndDomain(), "time", "Sleep") }

func (p *project) noDatabaseSQL() []finding {
	return p.importsMatching(p.appAndDomain(), func(ip string) bool {
		return ip == "database/sql" || strings.Contains(ip, "jmoiron/sqlx")
	})
}

func (p *project) errorfWrap() []finding {
	var out []finding

	for _, s := range p.files {
		if s.isTest {
			continue
		}

		alias := aliasOf(s.file, "fmt")
		if alias == "" {
			continue
		}

		ast.Inspect(s.file, func(n ast.Node) bool {
			call, ok := selectorCall(n, alias, "Errorf")
			if !ok || len(call.Args) == 0 {
				return true
			}

			if last, ok := call.Args[len(call.Args)-1].(*ast.Ident); !ok || last.Name != "err" {
				return true
			}

			if !slices.ContainsFunc(call.Args, func(a ast.Expr) bool {
				lit, ok := a.(*ast.BasicLit)

				return ok && lit.Kind == token.STRING && strings.Contains(lit.Value, "%w")
			}) {
				out = append(out, finding{s.rel, p.at(s, call) + "  fmt.Errorf(...) passes err without %w"})
			}

			return true
		})
	}

	return out
}

func (p *project) handlersNoPersistence() []finding {
	persistence := p.module + "/" + p.cfg.Layers.Persistence

	return p.importsMatching(p.productionIn(p.cfg.Layers.Handlers), func(ip string) bool {
		return under(ip, persistence)
	})
}

// layerImports holds the dependency direction: the domain imports none of the
// outer layers, the application none of infrastructure, interfaces or cmd. Test
// files are held to it too.
func (p *project) layerImports() []finding {
	l := p.cfg.Layers
	outer := func(dirs ...string) func(string) bool {
		return func(ip string) bool {
			return slices.ContainsFunc(dirs, func(d string) bool { return d != "" && under(ip, p.module+"/"+d) })
		}
	}

	var out []finding

	if l.Domain != "" {
		out = append(out, p.importsMatching(p.allIn(l.Domain), outer(l.Application, l.Infrastructure, l.Interfaces, l.Cmd))...)
	}

	if l.Application != "" {
		out = append(out, p.importsMatching(p.allIn(l.Application), outer(l.Infrastructure, l.Interfaces, l.Cmd))...)
	}

	return out
}

func (p *project) domainPurity() []finding {
	l := p.cfg.Layers
	srcs := p.productionIn(l.Domain)
	out := p.importsMatching(srcs, func(ip string) bool {
		return under(ip, ginImport) || strings.HasPrefix(ip, "gorm.io/") || ip == "database/sql" || strings.Contains(ip, "jmoiron/sqlx") ||
			ip == "encoding/json" || under(ip, "github.com/goccy/go-json") ||
			under(ip, datatableImport) || under(ip, resourceImport)
	})

	// datatable and resource are banned in test files of the domain too.
	for _, s := range p.files {
		if s.isTest && inTree(s.rel, l.Domain) {
			out = append(out, p.importsMatching([]source{s}, func(ip string) bool {
				return under(ip, datatableImport) || under(ip, resourceImport)
			})...)
		}
	}

	kernel := l.Domain + "/" + p.cfg.Kernel

	for _, s := range srcs {
		if inTree(s.rel, kernel) {
			continue
		}

		ast.Inspect(s.file, func(n ast.Node) bool {
			if st, ok := n.(*ast.StructType); ok && st.Fields != nil {
				for _, f := range st.Fields.List {
					if f.Tag != nil && strings.Contains(f.Tag.Value, "json:") {
						out = append(out, finding{s.rel, p.at(s, f) + "  has a json: tag"})
					}
				}
			}

			return true
		})
	}

	return out
}

func (p *project) applicationPurity() []finding {
	return p.importsMatching(p.productionIn(p.cfg.Layers.Application), func(ip string) bool {
		return under(ip, ginImport) || strings.HasPrefix(ip, "gorm.io/") || ip == "database/sql" || strings.Contains(ip, "jmoiron/sqlx")
	})
}

// noTimeNow finds every read of time.Now under dir, called or passed as a value
// (limiter{now: time.Now}).
func (p *project) noTimeNow(dir string) func() []finding {
	return func() []finding {
		var out []finding

		for _, s := range p.productionIn(dir) {
			alias := aliasOf(s.file, "time")
			if alias == "" {
				continue
			}

			ast.Inspect(s.file, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Now" {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
						out = append(out, finding{s.rel, p.at(s, sel)})
					}
				}

				return true
			})
		}

		return out
	}
}

// aggregate is the first folder under dir.
func aggregate(rel, dir string) string {
	first, rest, ok := strings.Cut(strings.TrimPrefix(rel, dir+"/"), "/")
	if !ok || rest == "" {
		return ""
	}

	return first
}

func (p *project) crossAggregate() []finding {
	l := p.cfg.Layers
	prefix := p.module + "/" + l.Domain + "/"

	var out []finding

	for _, s := range p.productionIn(l.Domain) {
		own := aggregate(s.rel, l.Domain)
		if own == "" {
			continue
		}

		for _, imp := range s.file.Imports {
			ip := importPath(imp)
			if !strings.HasPrefix(ip, prefix) {
				continue
			}

			other, _, _ := strings.Cut(strings.TrimPrefix(ip, prefix), "/")
			if other == "" || other == p.cfg.Kernel || other == own {
				continue
			}

			out = append(out, finding{s.rel, p.at(s, imp) + "  imports the " + other + " aggregate (" + ip + ")"})
		}
	}

	return out
}

// ctxFirst reports whether the first parameter is context.Context.
func ctxFirst(ft *ast.FuncType) bool {
	if ft.Params == nil || len(ft.Params.List) == 0 {
		return false
	}

	sel, ok := ft.Params.List[0].Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)

	return ok && pkg.Name == "context" && sel.Sel.Name == "Context"
}

func (p *project) domainContextFirst() []finding {
	var out []finding

	for _, s := range p.productionIn(p.cfg.Layers.Domain) {
		ast.Inspect(s.file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}

			iface, ok := spec.Type.(*ast.InterfaceType)
			if !ok || iface.Methods == nil {
				return true
			}

			for _, m := range iface.Methods.List {
				ft, ok := m.Type.(*ast.FuncType)
				if !ok || len(m.Names) == 0 || ctxFirst(ft) {
					continue // an embedded interface, or fine
				}

				out = append(out, finding{s.rel, p.at(s, m) + "  " + spec.Name.Name + "." + m.Names[0].Name + " does not take context.Context first"})
			}

			return true
		})
	}

	return out
}

func (p *project) serviceContextFirst() []finding {
	var out []finding

	for _, s := range p.productionIn(p.cfg.Layers.Application) {
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || !fn.Name.IsExported() {
				continue
			}

			if recv := receiverTypeName(fn.Recv.List[0].Type); !strings.HasSuffix(recv, p.cfg.ServiceType) || ctxFirst(fn.Type) {
				continue
			}

			out = append(out, finding{s.rel, p.at(s, fn) + "  " + receiverTypeName(fn.Recv.List[0].Type) + "." + fn.Name.Name + " does not take context.Context first"})
		}
	}

	return out
}

// receiverTypeName is the bare type name of a method receiver, without the
// pointer or the type parameters.
func receiverTypeName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}

	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return id.Name
		}
	}

	return ""
}
