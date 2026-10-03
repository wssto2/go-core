# Logic IDs: IAM-USER-005, IAM-USER-006, IAM-USER-007, IDENTITY-ADMIN-001, IDENTITY-ADMIN-002

Title: Administering accounts: create, update, deactivate, list, history, activity

Status: Approved (IAM-USER-005 to 007 moved from arv-next, `documentation/business-rules/iam/users.md`, their generic parts; IDENTITY-ADMIN-001 is the module's; IDENTITY-ADMIN-002 is arv-next's "Radnje" made generic, owner decision 2026-10-03)
Last updated: 2026-10-03
Module: `identity` (`identity/account` `Admin`, `identity/http` routes under `/v1/iam/users`, `identity/gormstore`, go-core `audit`)

## Summary

An administrator creates, edits, deactivates and finds people, unlocks them, reads their sign-in history and the
changes made to them, and ends their sessions. Who may is the routes' permission (`iam.user:view` reads,
`iam.user:manage` writes); the services trust their caller. What stays an application's: the records a person owns
(handed over before a deactivation: `DeactivationHook`), dealers and locations, "copy access from a colleague",
the names of the areas a person's activity is grouped in.

## IDENTITY-ADMIN-001 — Create, update and a new password

1. **Create** (`POST /v1/iam/users`, `iam.user:manage`) makes an **active** account. The login (at most 100
   characters, no white space, stored lower-case) and the address (one address, no display name, at most 255,
   stored lower-case) are normalised; the name is required (150), the phone optional (30), the locale a BCP-47 tag.
   A login in use is `409 identity.login.taken`, an address in use `409 identity.email.taken` (compared without
   case): **one account per address**, backed by a unique index (`uq_accounts_email`; an account without an address is NULL, so any number may have none), so a race past the check is also `identity.email.taken`, not a 500. The password must satisfy the `PasswordPolicy` (default: 8 characters, at most 72
   bytes, bcrypt's limit; `identity.WithPasswordPolicy` replaces it) or the answer is `identity.password.weak` with
   `params.rules`. Every refused field is also in `fields`, named as the input names it. The new person can sign in
   at once.
2. **Update** (`PUT /v1/iam/users/:id`) writes the fields that differ (login, name, e-mail, phone, locale) and nothing
   else: never the password hash, never the status. Nothing differing writes nothing and records nothing.
3. **A new password** (`PUT /v1/iam/users/:id/password`) is given to an account by an administrator: the policy
   applies, the lock is lifted (IAM-USER-002 item 7) and **every session of the person ends** (IAM-USER-004).
4. Creation, updates and the new password are on the person's change history (IAM-USER-007), the password by name
   only, **never by value**.
5. The facts are published to `Notices` after they are written: `AccountCreated`, `PasswordChanged`,
   `EmailChanged`.

Code: `identity/account/admin.go` (`Admin.Create`, `Update`, `SetPassword`), `identity/account/rules.go`.

## IAM-USER-005 — Deactivation (the generic part)

1. A person is deactivated by `POST /v1/iam/users/:id/deactivate` and made active again by `POST /v1/iam/users/:id/activate`
   (`iam.user:manage`); `active` is not part of the update. Nobody deactivates themselves
   (`400 identity.account.self_deactivation`); an inactive account is not deactivated again
   (`409 identity.account.already_inactive`), nor an active one activated (`409 identity.account.already_active`).
2. **The application takes part through `DeactivationHook`.** The hooks are asked, in order, with the account and
   the actor, **before anything is written, in the transaction of the deactivation**: one returning an error
   refuses the deactivation, that error reaches the caller as it is (give it a reason the client can explain), and
   nothing changes: the account stays active, its sessions stay, nothing is on its history. What a hook writes with
   the context it is given is in the same transaction, so a hand-over of records either happens together with the
   deactivation or not at all. arv-next's hand-over (leads, offers, contracts, appraisals; IAM-USER-005 items 2
   to 6 there) is such a hook and stays arv-next's.
3. In the same transaction the account is made inactive, **every session ends** (IAM-USER-004 item 6, with
   `signed_out_everywhere` on the history) and the deactivation is on the change history (IAM-USER-007).
4. `AccountDeactivated` and `AccountActivated` are published to `Notices` after commit.

Code: `identity/account/admin.go` (`Admin.Deactivate`, `Activate`, `DeactivationHook`).

## IAM-USER-006 — The list

1. `GET /v1/iam/users` answers go-core's datatable page (`data`, `meta?`, `total`, `per_page`, `current_page`,
   `last_page`, `from`, `to`; TypeScript `ListResult<UserRow>`). The views `view=active` (the default: active and
   not locked), `locked` (active and locked), `inactive` and `all`, each counted under the same search. `search`
   matches the login, the name and the address, without case, a `%` or `_` in it being itself. Sorting by
   `order_col` = `login` (default), `name`, `email`, `created_at` and `order_dir` = `asc` or `desc`; `per_page`
   at most 100 (default 20); an unknown view or column is a validation error.
   **The tabs' counts** are the page's `meta.views`, `[{key, count}]` for `active`, `locked`, `inactive` and `all`
   (TypeScript `ViewCount`, the one shape every list with tab counts uses, `datatable.ViewCount`), under the same
   `search` and whatever `view` is shown. They come from **one** query (`Accounts.Counts`: the accounts that match
   the search summed by status with `CASE`, the locked ones being the ids the lock history names), not one per view.
2. Each row carries the person's details, **last sign-in** (the latest `signed_in`, empty: never), the **lock**
   (`locked_until`, derived from the history as IAM-USER-002 says) and its status. The lock is asked only about
   people who had a wrong password within the lock's length, so a list costs one query per such person, not one per row.
3. Filters by dealer, location or role, and "access copied from a colleague" are applications'.

Code: `identity/account/admin.go` (`Admin.List`), `identity/gormstore/store.go` (`Accounts.Search`, `SignIns.LastSignIns`,
`SignIns.WrongPasswordsSince`).

## IAM-USER-007 — The change history

1. `GET /v1/iam/users/:id/changes` (`iam.user:view`) lists the changes made to the person, newest first, with how
   many there are: created, updated (the names of the changed fields with their before and after values),
   deactivated, activated, a new password, a changed e-mail address and a changed profile, each with who did it
   (`actor`, `{id, name}`, `null`: the person) and when.
   **Views** (`?view=`, ARV's Sve / Pristup / Podaci): `all` (also the empty one), `access` (what decides whether and
   how the person can get in: `password`, `deactivated`, `activated`) and `details` (every other action: `created`,
   `updated`, `email`, `profile` and any action another writer put on the account's rows). `meta.views` counts all
   three, whatever view is shown, from one `GROUP BY action`; a view that does not exist is
   `422 identity.history.view_invalid`. ARV's `access` also holds role bindings, impersonation and ended sessions: those
   are not on an account's change history in go-core (bindings are access's, sessions are on the sign-in history), so
   an application that wants them there writes them as audit rows of the account.
2. It is go-core's audit trail: a row of `audit_logs` of entity `account`, written in the transaction of the
   change. The table is `audit/migrations`', which `identity.Install` registers; its columns are what
   `audit.Migrate` creates, so a database that has the table adopts the file with `MarkApplied`.
3. **Only names and non-secret values are recorded**: a password change lists `password` as a changed field and no
   value; the audit package also masks keys that look secret.
4. A person's **sign-in history** (`GET /v1/iam/users/:id/signins`, IAM-USER-003) and **sessions** (`GET
   /v1/iam/users/:id/sessions`, `DELETE` one or all; IAM-USER-004) are read and ended by the same permissions
   (the sign-in history's views are IAM-USER-003 item 4). Wherever a row names a person by id (a change's `actor`, a
   session's `opened_by`, a sign-in row's `actor`, an activity row's `signed_in_as`) it is `{id, name}` (`PersonRef`),
   `null` when nobody: the names of a page come from one `Store.FindMany`, never one query per row, the name being the
   account's name or its login when it has none, and **empty when the account no longer exists** (the row keeps its
   id). What
   the person *did* (arv-next's "Radnje") is IDENTITY-ADMIN-002.

Code: `identity/account/admin.go` (`Admin.Changes`, `SignIns`, `Sessions`), `identity/gormstore/changelog.go`,
`audit/migrations`.

## IDENTITY-ADMIN-002 — A person's activity

arv-next's "Radnje" (IAM-USER-007 item 2 there), generic: what a person did, read from the audit trail.

1. `GET /v1/iam/users/:id/activity?area=&from=&to=&page=&per_page=` lists the audit rows **whose actor is the
   person**, newest first (then by id), as go-core's datatable page (`ListResult<ActivityRow>`; `per_page` at most
   100, default 20). A row says what (`record_type`, the audit trail's name for it, and `record_id`), what was
   done (`action`), in which `area`, and when (`created_at`, UTC). Nothing of a row's stored states is returned,
   so no secret can leak through it.
2. **The permission is `iam.user.activity:view`**, defined `System` (as in arv-next: only the people running the
   system hold it; the computed administrator role does). It is not part of `iam.user:view`.
   `identity.DefinePermissions` and `access.Install` define it in the catalogue when it lacks it; an application
   with its own catalogue defines it itself, since start-up checks every route's permission.
3. **`action` is one of `created`, `changed`, `deleted`**: the audit trail's verbs differ by writer (`create`,
   `created`, `delete`, `password`, `deactivated`), and `create`/`created` are `created`, `delete`/`deleted`
   `deleted`, everything else `changed`.
4. **Areas are the application's.** `identity.WithActivityAreas(identity.Area("crm").Types("customers").Prefix("contracts."))`
   maps record types, exactly or by prefix, to an area key; go-core ships none of an application's own. The keys are
   i18n keys of the application (lower case words, digits, `_`, `.`, at most 32, unique). The first area covering a
   type is its area (a filter by area shows exactly the rows that carry it); a type no area covers is in
   **`other`** (the key is reserved); the changes to accounts (`account`) are in **`identity`** unless the
   application names an area `identity` itself. A declaration that breaks these stops start-up and names the fix.
5. **Filters.** `area` is an area key, `other` or `identity`, empty for every one; an unknown key is
   `422 identity.activity.area_unknown` (`fields.area`). `from` and `to` are days, `YYYY-MM-DD`, **inclusive and
   UTC**; a day that is not one, or a range that ends before it starts, is `422 identity.activity.range_invalid`.
6. **Somebody signed in as the person (`signed_in_as`).** When the row was made while another person was signed in
   as them (IAM-USER-008: a session opened by `login-as`), `signed_in_as` is that person (`{id, name}`), else `null`: the row
   may be theirs, not the person's. It is derived, with no column of its own: a session opened by signing in as
   somebody is named `login-as:<actor>|<device>` in `tokens`, and it covers the time **from its opening to its last use
   (plus the minute a use is recorded at most once in)**, or to its expiry if that is sooner; where windows overlap
   the session opened last decides. A session ends on record only by being last used, so what the person did in a
   session of their own while one opened as them was still in use is marked too: the mark says "somebody was signed
   in as them", not "it was them".
7. **Counts per area** (ARV's tabs): the page's `meta.views` is `[{key, count}]`: `all` first, then each area the
   application named in order, `identity` (unless named) and `other`, counting the person's entries **within the
   same days** and **whatever `area` is shown**, so the tabs keep their numbers. They come from one `GROUP BY` of the
   record type over the actor and time index, folded into areas in the application's order. The datatable's `meta` is
   untyped, so the shape is documented here and the TypeScript declares `ViewCount` for the client to read it
   with; `all` and `other` are reserved keys.
8. **For arv-next's adoption:** the days are UTC (arv-next's Radnje read them in the server's local time), and
   `authz.binding` / `authz.role` rows are in `other` unless the application maps them to an area.
9. Not every module writes audit rows yet; what writes none is not in anybody's activity.

Code: `identity/account/activity.go` (`Admin.Activity`, `ActivityAreas`), `identity/gormstore/activity.go`
(`ActivityLog`, served by the index `idx_audit_logs_actor_created`), `identity/http/users.go` (`UserActivity`),
`identity/identity.go` (`WithActivityAreas`).
