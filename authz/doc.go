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
//   - [Admin]: delegation (no escalation), last-admin lock-out and cache
//     eviction around a [Store].
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
// # SQL not verified on MariaDB 10.3
//
// The test suites run on SQLite only. Written to be safe on MariaDB 10.3 but not
// yet run there:
//
//   - authzgorm.Filter output: equality, IN, IS NULL, AND/OR (no JSON functions,
//     CTEs or locking); plus any trusted expression the application supplies
//     (its own subqueries are the application's to check).
//   - gormstore.MySQLSchema and Migrate: the CHECK constraint on role_bindings
//     (enforced from MariaDB 10.2.1) and the six-column unique index.
//   - gormstore: SELECT ... FOR UPDATE on a role row (no SKIP LOCKED is used),
//     and duplicate-binding detection (a pre-check plus the unique index).
//
// # Concurrency
//
// A built [Engine] and its [Cache] are safe for concurrent use. A [Catalogue]
// is built at start-up, then frozen by [NewEngine]; do not define permissions
// afterwards.
package authz
