# Logic-ID mapping

The registry of the business rules go-core's modules own. Each line is one Logic ID, with the code that implements
it and the file that states the rule; every ID must be named in a comment at that code (`grep -r IAM-USER-001`
leads from the rule to the code and back). The test `TestLogicIDsAreAnchoredInCode` in this directory holds the
mapping to the code: an ID with no comment anchor, or a rule file whose ID is missing here, fails it.

A line starts with `- <ID> —` (an em dash); everything after it is for people. A module adds its lines here when
it adds a rule file under `docs/rules/<module>/`.

## identity

- IAM-USER-001 — one answer for every failed sign-in, and at most ten attempts per login per minute — `identity/account/signin_service.go` (`SignIn.Login`), `identity/account/attempts.go` — rules: `docs/rules/identity/signin.md`
- IAM-USER-002 — five wrong passwords lock sign-in for fifteen minutes, derived from the history — `identity/account/signin.go` (`Lock.LockedUntil`) — rules: `docs/rules/identity/signin.md`
- IAM-USER-003 — every sign-in event is on the person's history — `identity/account/signin.go` (`SignInEntry`), `identity/gormstore/store.go` — rules: `docs/rules/identity/signin.md`
- IAM-USER-004 — sessions are listed and ended, and ending all ends what the person opened as somebody else — `identity/account/users.go`, `identity/gormstore/store.go` — rules: `docs/rules/identity/signin.md`

## access

- IAM-AUTHZ-005 — roles and bindings are edited through the delegation rules — `access/admin/roles.go`, `access/admin/bindings.go`, `authz/admin.go` — rules: `docs/rules/access/authorization.md`
- ACCESS-ADMIN-001 — the module asks the application for names; an unknown subject is not found — `access/admin/admin.go` (`requireSubject`), `access/access.go` (`Seed`) — rules: `docs/rules/access/authorization.md`
