// Package authz is go-core's authorization engine: permissions bundled into
// roles, roles bound to a principal at a scope, and access denied unless a
// binding grants it.
//
//	Permission   crm.lead:view          declared once, in code (Catalogue)
//	    ⊂
//	Role         "Sales manager"        predefined (code) or custom (Store)
//	    ⊂
//	Binding      user 42 · role · location 7   who, which role, where
//
// go-core knows how access works. It never knows what an application's
// permissions, roles, places or record types are: those are declared by the
// application at start-up.
//
// # Building blocks
//
//   - [Catalogue]: the permissions, with metadata (sensitive, system,
//     organization-only, requires, ownable, feature, attributes).
//   - [Role]: a set of [Grant]s (permission + [Qualifier]) plus attribute
//     constraints; or a computed role ([All], [AllExcept]) that picks up new
//     permissions automatically.
//   - [Hierarchy] and [ScopeResolver]: the application's levels (for example
//     organization > dealer > location) and how a scope finds its parent.
//   - [Engine]: Require, RequireOn and Access. Deny by default; the union of
//     all bindings; the widest access wins.
//   - [Admin]: role and binding management around a [Store]. Saving a role is
//     strict (every grant must be held by the actor, as widely). Assigning a
//     role needs the bindings-management permission at a scope containing the
//     target, forbids System permissions the actor does not hold and
//     organization-only ones unless the actor manages bindings at the root, and
//     never lets anyone assign a role to themselves. It also guards the
//     last-admin lock-out and evicts the cache.
//
// Sub-packages: authzgorm (turn an [AccessSet] into a WHERE clause),
// authz/gormstore (a [Store] on GORM, audited), authzhttp (gin middleware and
// the /me/access payload), authzts (TypeScript output of a catalogue) and
// authztest (fakes and fixtures for an application's own tests).
//
// # Tenancy stays the hard wall
//
// A binding below the hierarchy's root pins a tenant (see
// [Hierarchy.WithTenantLevel]). Access clauses carry the whole chain from the
// binding's scope up to the root, and authzgorm.Filter constrains every level
// the table can express, so a dealer binding can never reach another dealer's
// rows even if a permission check has a bug. Only a root-scoped binding crosses
// tenants.
//
// Code still using tenancy.ScopeByTenant is kept safe by authzhttp.PinTenant,
// which fails closed: a root binding sets tenancy.WithAllTenants, exactly one
// tenant sets tenancy.WithTenantID, and anything else (several tenants, no
// bindings) sets nothing, so ScopeByTenant matches no rows.
//
// # Caching
//
// The effective-access [Cache] is per process. Admin evicts the affected
// entries when a role or binding changes, which is immediate for a single
// instance (arv-next runs one). With several instances, call Evict on each
// (for example from an event); the TTL ([DefaultCacheTTL]) is only the backstop
// for a change made by another process.
//
// # MariaDB 10.3
//
// The default test run uses SQLite. The SQL is also verified against a real
// MariaDB 10.3.39 by integration tests that skip unless AUTHZ_MARIADB_DSN is
// set (GOCORE_MARIADB_DSN works too, see database/dbtest). To run them:
//
//	docker run -d --rm --name authz-mariadb -e MARIADB_ROOT_PASSWORD=authzpw \
//	    -e MARIADB_DATABASE=authz -p 33063:3306 mariadb:10.3.39
//	# wait until: docker exec authz-mariadb mysqladmin -uroot -pauthzpw ping
//	AUTHZ_MARIADB_DSN='root:authzpw@tcp(127.0.0.1:33063)/' \
//	    go test -race ./authz/...
//	docker stop authz-mariadb
//
// They cover: the store conformance suite over gormstore.MySQLSchema and over
// gormstore.Migrate (with and without audit), including concurrent binds and
// role updates; the CHECK constraint and six-column unique index; the
// SELECT ... FOR UPDATE role lock; and the authzgorm property test (filtered
// rows equal RequireOn decisions), including expression-based owner and
// location. Each test runs in a throw-away schema it creates and drops on the
// server, so no existing database is touched (the user needs CREATE/DROP on databases).
//
// Nothing the package emits uses JSON_TABLE, SKIP LOCKED, FOR SHARE or
// FOR UPDATE OF. Not covered: MariaDB versions other than 10.3.39, MySQL 8, and
// the trusted SQL expressions an application supplies to authzgorm.Columns (their
// subqueries are the application's to check).
//
// # Concurrency
//
// A built [Engine] and its [Cache] are safe for concurrent use. A [Catalogue]
// is built at start-up, then frozen by [NewEngine]; do not define permissions
// afterwards.
package authz
