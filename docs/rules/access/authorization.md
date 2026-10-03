# Logic IDs: IAM-AUTHZ-005, ACCESS-ADMIN-001

Title: Roles and access are edited through the delegation rules, and nobody can go around them

Status: Approved (the rule moved from arv-next, `documentation/business-rules/iam/authorization.md`, IAM-AUTHZ-005, items 2 to 4; owner decisions 2026-09-30 and 2026-10-03)
Last updated: 2026-10-03
Module: `access` (`access/admin` services, `access/accesshttp` routes), over `authz` (`authz.Admin`, `authz.Engine`)

## Summary

Roles and bindings are changed only by the administration services, which ask the engine whether the acting
person may. The delegation rules are `authz.Admin`'s and cannot be configured away: nobody gives more than
they hold, nobody gives to themselves, nobody removes their own last access to the binding permission. The
module adds what an editor needs on top: who holds a role, how two roles differ, what a person may do and
why, where the actor may give roles and which roles.

The permission ids the module is guarded by are fixed: `iam.role:{view,manage,delete}`, `iam.user:{view,manage}`.

## IAM-AUTHZ-005 — Roles and bindings are edited through the delegation rules

1. **Predefined roles are read-only.** A role defined in code (identified by its key; computed ones derive their
   grants from the catalogue) cannot be updated or deleted: refused with 403, reason `authz.forbidden`,
   `why: predefined`. A custom role is addressed by its ID (`ref` is the key of a predefined role or the ID of a
   custom one).
2. **Building a custom role** needs `ManageRoles`. Every grant must be one the actor holds at least as widely and
   as broadly (qualifier and attribute constraints), otherwise `authz.escalation`; a System permission the actor
   lacks is never given. The role must validate against the catalogue, and the editor is told every problem at
   once (`authz.invalid`, `params.problems`): unknown permission, a qualifier on a permission that is not
   ownable, an unmet `Requires` (including a required grant that is narrower), duplicates, unknown or empty
   attribute constraints. A qualifier is `own`, `own_location` or `all`.
3. **Deleting a role is its own permission** (`DeleteRoles`; `ManageRoles` does not include it) and is refused
   while the role is bound (409, `authz.role_in_use`, `params.bindings`).
4. **Compare** lists, for a custom role against a predefined one, the grants only in either role and the grants at
   different qualifiers. **Replace** re-binds every holder of a custom role to a predefined role at the same scope,
   through the delegation rules and in one transaction: a holder the actor may not give the role to (the actor
   themselves included) refuses the whole replacement and nothing changes. The custom role stays, unbound, and can
   then be deleted. The engine's cache is evicted for the role and each holder after the commit.
5. **A binding** needs `ManageBindings` held at a scope that contains the target scope; the role may contain a
   System permission only if the actor holds it there, and an organization-only one only if the actor holds
   `ManageBindings` at the root (and it can only be bound there). Nobody assigns a role to themselves
   (`authz.self_assignment`). Nobody removes their own last access to `ManageBindings` (`authz.last_admin`); two
   removals of the same person's two bindings at the same moment cannot both go through (the check and the write
   are serialised per acting person, within one process). Removing a binding needs the same rights as creating it.
   The subject's cache is evicted, so a change applies on their next request.
6. **Effective access with the why.** For a subject, the bindings and, for each permission they hold, every way
   they hold it: qualifier, place, attribute constraints and the binding and role it comes from; `unavailable`
   for a permission a role grants but a feature switch turns off. It equals what the engine says for that subject.
   Reading it needs `ViewAccess`; `can_manage` says whether the actor may change the subject's bindings (they
   hold `ManageBindings` somewhere and the subject is not themselves).
7. **Scope options and bindable roles offer only what the delegation accepts.** The places are those where the
   actor holds `ManageBindings` (the root, or the places within their clauses); the bindable roles at a place are
   the roles for which the delegation would accept the binding (a custom role with a System permission the actor
   lacks, an organization-only role below the root and the like are left out). A place that does not exist is a
   404, a scope that does not fit the hierarchy a 400.
8. **Holders** of a role are counted once per person or service account however many places they hold it at, and
   listed once per binding, by name and place, ordered by name.
9. **Audit.** When the application gives the module an audit repository, every role and binding change is
   audited with its before and after state, in the transaction of the change.

## ACCESS-ADMIN-001 — The module asks the application for names, and a person it does not know is not found

The module owns no users or places. Names come through two ports: `SubjectDirectory` (people and service accounts)
and `ScopeCatalog` (places below the root; it is also the engine's scope resolver). A subject missing from the
directory is a 404 for reading its access, binding and unbinding, so an id that does not exist cannot be told
apart from one the actor may not see. A place missing from the catalogue is shown without a name. An application
without tenancy has the root, `organization`, as its only level and uses `access.NoScopes()`.

`Seed` gives the first administrator a predefined role at the root outside the delegation rules (a seed or a
command, never an HTTP route); it does nothing when they already hold it.

## Code

- IAM-AUTHZ-005: `access/admin/{roles,bindings,views}.go`, `authz/{admin,delegation}.go`, routes `access/accesshttp/routes.go`.
- ACCESS-ADMIN-001: `access/admin/{ports,admin}.go`, `access/access.go` (`Seed`).
