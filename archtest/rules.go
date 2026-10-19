package archtest

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// rules lists the checks Config enables, in a stable order.
func (p *project) rules() []rule {
	set := &ruleSet{proj: p, rules: nil}

	p.conventionRules(set)
	p.layerRules(set)
	p.dataRules(set)
	p.routeRules(set)
	p.portabilityRules(set)
	p.personOnlyRules(set)

	if len(p.cfg.Banned) > 0 && !slices.Contains(p.cfg.Skip, bannedRule) {
		set.rules = append(set.rules, rule{name: bannedRule, check: p.banned})
	}

	return set.rules
}

func (p *project) conventionRules(set *ruleSet) {
	layers := p.cfg.Layers
	inner := layers.Domain != "" || layers.Application != ""
	handlers := layers.Handlers != "" && layers.Persistence != "" && layers.Handlers != layers.Persistence

	set.add(inner, NoPanic, "no panic in application/domain",
		"return an error so the caller decides", p.noPanic)
	set.add(layers.Application != "", NoContextBackground, "no context.Background() in the application layer",
		"accept ctx from the caller so cancellation, deadlines and tenancy/auth values propagate",
		p.noContextBackground)
	set.add(inner, NoSleep, "no time.Sleep in application/domain",
		"use a ctx-bound timer or ticker, or move scheduling to the caller", p.noSleep)
	set.add(inner, NoDatabaseSQL, "no database/sql outside infrastructure",
		"keep persistence in the infrastructure layer behind domain repository interfaces", p.noDatabaseSQL)
	set.add(true, ErrorfWrap, "fmt.Errorf wrapping err must use %w",
		"use %w so errors.Is/As keep working across the wrap", p.errorfWrap)
	set.add(handlers, HandlersNoPersistence, "handlers must not import repository implementations",
		"call an application service; the persistence package is wired in the composition root",
		p.handlersNoPersistence)
}

func (p *project) layerRules(set *ruleSet) {
	layers := p.cfg.Layers
	inner := layers.Domain != "" || layers.Application != ""
	domain, app := layers.Domain != "", layers.Application != ""

	set.add(inner, LayerImports, "layers import inward only",
		"the domain is the innermost layer and the application depends on the domain only: keep the type pure and "+
			"map to the outer-layer representation at the boundary, or depend on a domain interface that the "+
			"composition root implements", p.layerImports)
	set.add(domain, DomainPurity, "the domain is framework-free",
		"no web framework, ORM, encoding/json, go-core datatable/resource or json: tag in the domain: "+
			"marshal in the adapter and map at the boundary (the shared kernel may carry json tags)",
		p.domainPurity)
	set.add(app, ApplicationPurity, "the application layer is framework-free",
		"no web framework or ORM in the application layer: keep gin in the handlers and "+
			"*gorm.DB in infrastructure, expose a transaction port from the domain", p.applicationPurity)
	set.add(domain, NoTimeNowDomain, "no time.Now() in the domain",
		"take a Clock dependency and call clock.Now() so tests can control time", p.noTimeNow(layers.Domain))
	set.add(app, NoTimeNowApplication, "no time.Now() in the application",
		"take a Clock in the constructor and call clock.Now()", p.noTimeNow(layers.Application))
	set.add(domain, CrossAggregate, "no cross-aggregate domain imports",
		"reference the other aggregate by ID and compose it in the application layer", p.crossAggregate)
	set.add(domain, DomainContextFirst, "domain interface methods take context.Context as the first parameter",
		"thread ctx from the caller so cancellation, deadlines and tenancy propagate; a genuinely context-free port "+
			"(a clock, a pure policy) goes on this rule's Allowed list with a note in your architecture document",
		p.domainContextFirst)
	set.add(app, ServiceContextFirst,
		"exported application service methods take context.Context as the first parameter",
		"accept ctx from the handler and pass it to the repository", p.serviceContextFirst)
}

func (p *project) dataRules(set *ruleSet) {
	layers := p.cfg.Layers
	shape := p.cfg.Repository

	set.add(layers.Application != "", BoundedStrings, "every string input field declares a max: bound",
		"an unbounded string reaches its VARCHAR(n) column unchecked - an over-long value is a 500, not a clean "+
			`422. Add the bound to the validation tag, sized to the backing column, e.g. validation:"required|max:150"`+
			" - it then propagates to the generated Zod schema", p.boundedStrings)
	set.add(layers.Application != "", ValidationRules, "validation rules are known",
		"a validation tag that names a rule go-core's validator does not know (gte=0 for min:0, a typo) fails "+
			"every request of that input with 500 and no hint", p.validationRules)
	set.add(layers.Domain != "" && layers.Application != "", ServiceRepositories,
		"services inject only their own aggregate's repositories",
		"obtain that domain's data through its Service, not its Repository", p.serviceRepositories)
	set.add(shape.Dir != "" && (shape.TxFile != "" || shape.ScopeFile != ""), RepositoryShape, "repository shape",
		"a repository reaches the caller's transaction and tenant scope through the one file of the package "+
			"that owns them", p.repositoryShape)
}

func (p *project) portabilityRules(set *ruleSet) {
	set.add(len(p.cfg.SQL.Dirs) > 0, SQLPortability, "SQL that MariaDB 10.3.39 rejects",
		"every market runs MariaDB 10.3.39, which rejects SKIP LOCKED, FOR UPDATE OF, FOR SHARE, JSON_TABLE, ->> and "+
			"RETURNING while a local MySQL accepts them: use a single-table SELECT ... FOR UPDATE by primary key "+
			"instead of SKIP LOCKED / FOR UPDATE OF, and FOR UPDATE or LOCK IN SHARE MODE instead of FOR SHARE",
		p.sqlPortability)
}

func (p *project) routeRules(set *ruleSet) {
	set.add(true, RoutePaths, "route paths are kebab-case",
		"every segment of a route.Get/Post/Put/Patch/Delete/Raw path is lowercase words joined by -; "+
			":param and *param are allowed", p.routePaths)
	set.add(true, RouteIdentifiers, "route groups and permissions are single lowercase words",
		"name each segment of a route group, each dot-separated namespace of a permission and its verb as one "+
			"lowercase word with no separators; action sub-paths on a route are exempt", p.routeIdentifiers)
	set.add(true, RouteAccess, "every route declares who may call it",
		"end the declaration chain with Requires, RequiresAny, Public, PersonOnly or Authenticated; "+
			"an undeclared route is open to every signed-in caller", p.routeAccess)
}

func (p *project) personOnlyRules(set *ruleSet) {
	set.add(len(p.cfg.PersonOnly.Areas) > 0, RoutePersonOnly, "routes of a person-only area are PersonOnly",
		"end the declaration chain of every route in a PersonOnly.Areas area with PersonOnly() (or Public() for a route "+
			"with nobody to refuse yet, such as sign-in), so a service account's key is refused on it; an area with no "+
			"route is a typo or a renamed path: fix the area, or list it in PersonOnly.External when a library serves it",
		p.routePersonOnly)
}

func (p *project) appAndDomain() []Source {
	return p.productionIn(p.cfg.Layers.Application, p.cfg.Layers.Domain)
}

func (p *project) noPanic() []finding {
	var out []finding

	for _, src := range p.appAndDomain() {
		ast.Inspect(src.File, func(node ast.Node) bool {
			if call, isCall := node.(*ast.CallExpr); isCall && isIdent(call.Fun, "panic") {
				out = append(out, finding{key: src.Rel, text: src.At(call) + "  panic("})
			}

			return true
		})
	}

	return out
}

func isIdent(expr ast.Expr, name string) bool {
	ident, found := expr.(*ast.Ident)

	return found && ident.Name == name
}

func (p *project) noContextBackground() []finding {
	return callsOf(p.productionIn(p.cfg.Layers.Application), "context", "Background")
}

func (p *project) noSleep() []finding { return callsOf(p.appAndDomain(), "time", "Sleep") }

func (p *project) noDatabaseSQL() []finding {
	return importsMatching(p.appAndDomain(), func(path string) bool {
		return path == "database/sql" || strings.Contains(path, "jmoiron/sqlx")
	})
}

func (p *project) errorfWrap() []finding {
	var out []finding

	for _, src := range p.files {
		alias := aliasOf(src.File, "fmt")
		if src.IsTest || alias == "" {
			continue
		}

		ast.Inspect(src.File, func(node ast.Node) bool {
			if call, isErrorf := SelectorCall(node, alias, "Errorf"); isErrorf && passesErrUnwrapped(call) {
				out = append(out, finding{key: src.Rel, text: src.At(call) + "  fmt.Errorf(...) passes err without %w"})
			}

			return true
		})
	}

	return out
}

// passesErrUnwrapped reports fmt.Errorf(format, ..., err) whose format has no %w.
func passesErrUnwrapped(call *ast.CallExpr) bool {
	if len(call.Args) == 0 || !isIdent(call.Args[len(call.Args)-1], "err") {
		return false
	}

	return !slices.ContainsFunc(call.Args, func(arg ast.Expr) bool {
		lit, isLit := arg.(*ast.BasicLit)

		return isLit && lit.Kind == token.STRING && strings.Contains(lit.Value, "%w")
	})
}

func (p *project) handlersNoPersistence() []finding {
	persistence := p.module + "/" + p.cfg.Layers.Persistence

	return importsMatching(p.productionIn(p.cfg.Layers.Handlers), func(path string) bool {
		return under(path, persistence)
	})
}
