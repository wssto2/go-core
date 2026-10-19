package archtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The rule names, for Config.Allowed and Config.Skip.
const (
	NoPanic               = "no-panic"
	NoContextBackground   = "no-context-background"
	NoSleep               = "no-sleep"
	NoDatabaseSQL         = "no-database-sql"
	ErrorfWrap            = "errorf-wrap"
	HandlersNoPersistence = "handlers-no-persistence"
	LayerImports          = "layer-imports"
	DomainPurity          = "domain-purity"
	ApplicationPurity     = "application-purity"
	NoTimeNowDomain       = "no-time-now-domain"
	NoTimeNowApplication  = "no-time-now-application"
	CrossAggregate        = "cross-aggregate"
	DomainContextFirst    = "domain-context-first"
	ServiceContextFirst   = "service-context-first"
	BoundedStrings        = "bounded-strings"
	RoutePaths            = "route-paths"
	RouteIdentifiers      = "route-identifiers"
	ValidationRules       = "validation-rules"
	RouteAccess           = "route-access"
	ServiceRepositories   = "service-repositories"
	RepositoryShape       = "repository-shape"
	SQLPortability        = "sql-portability"
	RoutePersonOnly       = "route-person-only"
)

// bannedRule is the name of the Config.Banned checks in a Result; they have no
// ratchet and cannot be allowed.
const bannedRule = "banned"

// The names of the Config.FieldWrites, RequiresCall and ContextRoots checks in a Result; like
// Banned, they have no ratchet.
const (
	fieldWritesRule  = "field-writes"
	requiresCallRule = "requires-call"
	contextRootsRule = "context-roots"
)

// ruleNames lists every rule Allowed and Skip may name.
func ruleNames() []string {
	return []string{
		NoPanic, NoContextBackground, NoSleep, NoDatabaseSQL, ErrorfWrap, HandlersNoPersistence, LayerImports,
		DomainPurity, ApplicationPurity, NoTimeNowDomain, NoTimeNowApplication, CrossAggregate, DomainContextFirst,
		ServiceContextFirst, BoundedStrings, RoutePaths, RouteIdentifiers, ValidationRules, RouteAccess,
		ServiceRepositories, RepositoryShape, SQLPortability, RoutePersonOnly, bannedRule,
		fieldWritesRule, requiresCallRule, contextRootsRule,
	}
}

// Layers are the directories of the module's layers, relative to its root. A
// layer that is empty is absent: the rules that need it are not run.
type Layers struct {
	Domain         string // entities and the ports (repository interfaces); framework-free
	Application    string // services, inputs, policies; depends on the domain only
	Infrastructure string // adapters: the database, HTTP, integrations
	Interfaces     string // delivery, when it is a layer of its own
	Cmd            string // the binaries; no inner layer may import them

	// Handlers is where the HTTP handlers and routes live; it defaults to
	// Interfaces, else Infrastructure+"/http". Persistence is where the
	// repositories live and defaults to Infrastructure+"/db". Handlers must not
	// import Persistence.
	Handlers    string
	Persistence string
}

// Config describes the module being checked.
type Config struct {
	// Root is the module root (the directory of go.mod). Empty finds it from
	// the working directory upward, which is where a test runs.
	Root string

	Layers Layers

	// Allowed is the ratchet: per rule name, the files that may still break
	// the rule (ServiceRepositories: "file|domain/path", the domain path being
	// relative to Layers.Domain). A new violation and a stale entry both fail.
	Allowed map[string][]string

	// Skip switches rules off by name.
	Skip []string

	// Banned adds the application's own bans. They have no ratchet.
	Banned []Banned

	// Kernel is the folder of Layers.Domain that every aggregate may import
	// (the shared kernel) and that may carry json tags. Default "shared".
	Kernel string

	// SharedDomains are the domains whose repositories a service of any other
	// domain may inject (reference data). The Kernel is always shared.
	SharedDomains []string

	// OwnedDomains maps a folder of Layers.Application to the domain folders
	// it owns, when they are not just the same name.
	OwnedDomains map[string][]string

	// Inputs are the method names that make a type of Layers.Application an
	// input struct for BoundedStrings: it must have every one. Default
	// {"Validate"}.
	Inputs []string

	// ServiceType is the type-name suffix of the application services whose
	// exported methods ServiceContextFirst checks. Default "AppService".
	ServiceType string

	// Repository is the shape RepositoryShape holds the repository package to.
	// A field left empty leaves its part of the rule off.
	Repository Repository

	// SQL switches on SQLPortability: the Go sources of these directories may
	// not carry SQL that MariaDB 10.3 rejects.
	SQL SQL

	// PersonOnly switches on RoutePersonOnly.
	PersonOnly PersonOnly

	// FieldWrites hold a field to the few places that may assign it.
	FieldWrites []FieldWrites

	// RequiresCall holds a file that calls one function to calling another too.
	RequiresCall []RequiresCall

	// ContextRoots bans context.Background() and context.TODO() as a root context in
	// directories of the application's own, with the wrappers that make one acceptable.
	ContextRoots []ContextRoots
}

// PersonOnly names the areas of the API that only make sense for a person (a
// session, a profile, preferences, notifications): a service account's key
// must be refused on every route in them.
type PersonOnly struct {
	// Prefix is what the server mounts the declared paths under ("/api"); it
	// is put before every declared path before the areas are compared.
	Prefix string
	// Areas are the path prefixes, Prefix included ("/api/v1/home"). A route
	// whose full path is an area or under it declares PersonOnly or Public.
	// Empty leaves the rule off.
	Areas []string
	// External are Areas served by a library, so the module declares no route
	// in them. Without it an area with no route in the sources fails, because
	// a typo or a renamed path would go on protecting nothing.
	External []string
}

// Repository names the one file of the repository package that may use a
// plumbing API of go-core, so every repository reaches it the same way.
type Repository struct {
	// Dir is the repository package, relative to the root; the rule checks the
	// production files of that package only.
	Dir string
	// TxFile is the only file that may call database.TxFromContext,
	// SQLTxFromContext, NewTransactor and NewTransactorFromRegistry.
	TxFile string
	// ScopeFile is the only file that may call tenancy.ScopeByTenant.
	ScopeFile string
	// NoNestedTransaction bans .Transaction( on a *gorm.DB in Dir: a transaction opens
	// with the BaseRepository's WithinTx, because a nested Transaction opens a
	// savepoint and leaves tx out of the context, so a helper called inside it
	// uses the outer connection.
	NoNestedTransaction bool
}

// Banned is a rule of the application's own: an import, or one symbol of a
// package, that must not appear.
type Banned struct {
	// Import is the import path. It matches the path exactly, a trailing
	// "/..." matches the path and everything under it, and "..." alone matches
	// every package outside the standard library.
	Import string
	// Symbol, when set, bans only that member of the package (database.NewTransactor),
	// found by the name the file imports the package under.
	Symbol string
	// Method, when set, bans a method of that name called on a value (app.Modules()),
	// found in the files that import Import; a member of an imported package of the
	// same name is not a method. Import is then the package the value's type comes
	// from, which scopes the ban to the files that use it.
	Method string
	// Literal, when set, bans a composite literal with at least one element of that
	// type of the package (permission.Permission{Verb: "read"}), found by the name the
	// file imports the package under. The empty literal is allowed.
	// Symbol, Method and Literal exclude each other.
	Literal string
	// Why is the sentence a failure ends with: what to do instead.
	Why string

	// In limits the ban to these places, relative to the root: a file, a
	// package directory, or "dir/..." for a directory tree. Empty is the whole module.
	In []string
	// Except are places (as In) where the ban does not apply.
	Except []string
	// Unless are imports (as Import) a "..." or "/..." ban lets through.
	Unless []string
	// Tests also applies the ban to _test.go files and test-support packages
	// (a directory named *test). Default: production code only.
	Tests bool
}

// named lists the configured directories by field name.
func (l Layers) named() map[string]string {
	return map[string]string{
		"Domain": l.Domain, "Application": l.Application, "Infrastructure": l.Infrastructure,
		"Interfaces": l.Interfaces, "Cmd": l.Cmd, "Handlers": l.Handlers, "Persistence": l.Persistence,
	}
}

// withDefaults fills Handlers and Persistence from the conventional folders.
func (l Layers) withDefaults(root string) Layers {
	if l.Handlers == "" {
		l.Handlers = firstExisting(root, l.Interfaces, join(l.Infrastructure, "http"))
	}

	if l.Persistence == "" {
		l.Persistence = firstExisting(root, join(l.Infrastructure, "db"))
	}

	return l
}

func join(dir, sub string) string {
	if dir == "" {
		return ""
	}

	return dir + "/" + sub
}

// firstExisting is the first non-empty candidate that is a directory of root.
func firstExisting(root string, candidates ...string) string {
	for _, dir := range candidates {
		if dir == "" {
			continue
		}

		info, err := os.Stat(filepath.Join(root, dir))
		if err == nil && info.IsDir() {
			return dir
		}
	}

	return ""
}

// validate refuses a ban that is ambiguous or cannot find what it bans.
func (b Banned) validate() error {
	forms := 0

	for _, set := range []bool{b.Symbol != "", b.Method != "", b.Literal != ""} {
		if set {
			forms++
		}
	}

	switch {
	case forms > 1:
		return fmt.Errorf("%w: Banned %q may set only one of Symbol, Method and Literal", errConfig, b.Import)
	case forms == 1 && (b.Import == "" || strings.Contains(b.Import, "...")):
		return fmt.Errorf("%w: Banned Symbol, Method and Literal need an Import that names one package (Method needs an Import)", errConfig)
	}

	return nil
}

// FieldWrites holds a field of a struct to the places that may assign it: an assignment, an
// increment, or (Literal) a keyed element of a composite literal of the type. Without type
// information the value is found by the name it is selected by, x.Type.Field, which is the
// type's own name when the group is embedded or named after it (v.Workflow.Phase).
type FieldWrites struct {
	// Type is the name the struct is selected by: the Workflow of v.Workflow.Phase.
	Type string
	// Field is the field; empty is every field of Type.
	Field string
	// Only are the places that may write it: a file, a package directory, "dir/..." for a tree,
	// each optionally followed by ":Func" to allow that function (or method) name only,
	// as in "internal/domain/vehicle/vehicle.go:transitionPhase". Empty allows nowhere.
	Only []string
	// Literal also bans Field (any field when empty) as a key of a Type{...} literal. Package,
	// the directory of the package that declares Type, tells it from another package's Type:
	// a bare Type{...} counts in that package only, pkg.Type{...} where pkg imports it.
	Literal bool
	Package string
	// Why is the sentence a failure ends with: what to do instead.
	Why string
	// In, Except and Tests as Banned's: where the rule applies (default the whole module,
	// production code only).
	In, Except []string
	Tests      bool
}

// RequiresCall holds every file that calls Call to call Also as well: a route group behind
// authentication that never establishes a tenant, say. Both are "import/path.Member"; the file
// is checked under whatever name it imports the package by.
type RequiresCall struct {
	Call, Also string
	// Why is the sentence a failure ends with: what to do instead.
	Why string
	// In, Except and Tests as Banned's.
	In, Except []string
	Tests      bool
}

// ContextRoots bans context.Background() and context.TODO() in Dirs, except as the first argument
// of a wrapper. An application's code that runs for nobody in particular (a worker, a command, a
// start-up read) has no request to take a context from, and the wrapper gives it the one it needs.
type ContextRoots struct {
	// Dirs are the trees the ban covers, relative to the root.
	Dirs []string
	// Wrappers are the calls that may take the root as their first argument: "import/path.Func"
	// for a package function, ".Method" for a method of any value (".InfoContext").
	Wrappers []string
	// Why is the sentence a failure ends with: what to do instead.
	Why string
	// Tests also covers _test.go files and test-support packages.
	Tests bool
}
