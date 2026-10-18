package archtest

import (
	"os"
	"path/filepath"
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

// ruleNames lists every rule Allowed and Skip may name.
func ruleNames() []string {
	return []string{
		NoPanic, NoContextBackground, NoSleep, NoDatabaseSQL, ErrorfWrap, HandlersNoPersistence, LayerImports,
		DomainPurity, ApplicationPurity, NoTimeNowDomain, NoTimeNowApplication, CrossAggregate, DomainContextFirst,
		ServiceContextFirst, BoundedStrings, RoutePaths, RouteIdentifiers, ValidationRules, RouteAccess,
		ServiceRepositories, RepositoryShape, SQLPortability, RoutePersonOnly, bannedRule,
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
