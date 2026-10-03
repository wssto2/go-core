# Logic-ID mapping: access

The lines for `docs/rules/mapping.md` (P1 owns that file; merge these into it). Each ID is anchored by a comment at
the code that implements it.

- IAM-AUTHZ-005 — roles and bindings are edited through the delegation rules — `access/admin/roles.go`, `access/admin/bindings.go`, `authz/admin.go` — rules: `docs/rules/access/authorization.md`
- ACCESS-ADMIN-001 — the module asks the application for names; an unknown subject is not found — `access/admin/admin.go` (`requireSubject`), `access/access.go` (`Seed`) — rules: `docs/rules/access/authorization.md`
