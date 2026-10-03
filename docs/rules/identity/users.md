# Logic IDs: IAM-USER-005, IAM-USER-006, IAM-USER-007, IDENTITY-ADMIN-001

Title: Administering accounts: create, update, deactivate, list, history

Status: Approved (IAM-USER-005 to 007 moved from arv-next, `documentation/business-rules/iam/users.md`, their generic parts; IDENTITY-ADMIN-001 is the module's)
Last updated: 2026-10-03
Module: `identity` (`identity/account` `Admin`, `identity/http` routes under `/v1/iam/users`, `identity/gormstore`, go-core `audit`)

## Summary

An administrator creates, edits, deactivates and finds people, unlocks them, reads their sign-in history and the
changes made to them, and ends their sessions. Who may is the routes' permission (`iam.user:view` reads,
`iam.user:manage` writes); the services trust their caller. What stays an application's: the records a person owns
(handed over before a deactivation: `DeactivationHook`), dealers and locations, "copy access from a colleague",
the actions a person has taken (arv-next's "Radnje").

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
2. Each row carries the person's details, **last sign-in** (the latest `signed_in`, empty: never), the **lock**
   (`locked_until`, derived from the history as IAM-USER-002 says) and its status. The lock is asked only about
   people who had a wrong password within the lock's length, so a list costs one query per such person, not one per row.
3. Filters by dealer, location or role, and "access copied from a colleague" are applications'.

Code: `identity/account/admin.go` (`Admin.List`), `identity/gormstore/store.go` (`Accounts.Search`, `SignIns.LastSignIns`,
`SignIns.WrongPasswordsSince`).

## IAM-USER-007 — The change history

1. `GET /v1/iam/users/:id/changes` (`iam.user:view`) lists the changes made to the person, newest first, with how
   many there are: created, updated (the names of the changed fields with their before and after values),
   deactivated, activated, a new password, a changed e-mail address and a changed profile, each with who did it (empty:
   the person) and when.
2. It is go-core's audit trail: a row of `audit_logs` of entity `account`, written in the transaction of the
   change. The table is `audit/migrations`', which `identity.Install` registers; its columns are what
   `audit.Migrate` creates, so a database that has the table adopts the file with `MarkApplied`.
3. **Only names and non-secret values are recorded**: a password change lists `password` as a changed field and no
   value; the audit package also masks keys that look secret.
4. A person's **sign-in history** (`GET /v1/iam/users/:id/signins`, IAM-USER-003) and **sessions** (`GET
   /v1/iam/users/:id/sessions`, `DELETE` one or all; IAM-USER-004) are read and ended by the same permissions. arv-next's
   "Radnje" (the actions a person took across modules) is not here: it needs every module to write audit rows.

Code: `identity/account/admin.go` (`Admin.Changes`, `SignIns`, `Sessions`), `identity/gormstore/changelog.go`,
`audit/migrations`.
