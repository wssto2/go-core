# Logic IDs: IAM-USER-001, IAM-USER-002, IAM-USER-003, IAM-USER-004

Title: Sign-in answers alike, locks after wrong passwords, is recorded, and sessions can be listed and ended

Status: Approved (the rules moved from arv-next, `documentation/business-rules/iam/users.md`, IAM-USER-001 to 004; owner decisions 2026-09-30 and 2026-10-03)
Last updated: 2026-10-03
Module: `identity` (`identity/account` services, `identity/http` routes, `identity/gormstore` tables)

## Summary

A person signs in with a login and a password. Every refusal that is not the lock looks the same from outside,
five wrong passwords in a row lock sign-in for fifteen minutes, every attempt is on the person's history, and the
person (or an administrator, in the users module) can see and end their sessions. The numbers are defaults of
`account.Config`; the rules are not switchable.

What is not here: the users list and its "locked" view, unlocking, the person's own history screen and ending
sessions on a new password or a deactivation. Those belong to the users module, which uses the same
services and tables.

## IAM-USER-001 — One answer for every failed sign-in

1. A sign-in with an **unknown login** and one with a **wrong password** get exactly the same response: `422`,
   reason `identity.signin.failed`, no params. Both do the same work: an unknown login's password is compared
   against a placeholder hash made by the same `PasswordHasher`, so the time does not tell them apart either.
2. The **lock** (IAM-USER-002) is the one exception: `422`, reason `identity.signin.locked`, `params.locked_until`
   (RFC 3339). Whoever sees it has made five wrong guesses at that login.
3. Whether an account is **inactive** is said only to whoever gave its right password: `400`, reason
   `identity.signin.inactive`. A wrong password for an inactive account is `identity.signin.failed`. Everything else
   that resolves a session (`Authenticate`, `Refresh`) only finds active accounts.
4. An unknown login is not recorded (there is nobody to attach it to).
5. **At most ten attempts per login per minute** (`AttemptsPerMinute`, a fixed one-minute window, the login
   compared without case or surrounding space), whatever the answers and whether the login exists: the eleventh
   is answered like a lock (`identity.signin.locked`, `params.locked_until` = the end of the window), without
   reading the account, comparing the password or writing to the history. It is what keeps parallel attempts from
   getting far past the five of IAM-USER-002 before the lock is recorded. The counter is in memory, so per process
   (a `ponytail:` note in the code): with several instances a person may get ten per instance per minute; the lock,
   read off the history, is the shared limit.

Code: `identity/account/signin_service.go` (`SignIn.Login`), `identity/account/attempts.go`, `identity/account/errors.go`.

## IAM-USER-002 — The lock after five wrong passwords

1. **Derived, never stored.** The lock is read off the sign-in history by one function, `account.Lock.LockedUntil`.
   It reads the person's latest five **lock events** (`wrong_password`, `signed_in`, `unlocked`), newest first:
   when there are five and all are `wrong_password`, sign-in is locked until the newest of them plus 15 minutes.
   Five and fifteen are `Config.Lock`.
2. A sign-in or an unlock among them starts the count again. A refused attempt while locked (`locked_out`) is on
   the record but is not a lock event: its password was never checked, and it does not extend the lock.
3. After a lock has run out, the next wrong password locks again for 15 minutes (the latest five are still all
   wrong), until the person signs in or is unlocked.
4. The sign-in checks the lock **before** the password: while locked even the right password is refused
   (`locked_out`) and not compared.
5. Parallel guesses: the lock is derived, so requests that arrive together are each checked before the first of
   them is recorded; the per-login limit of IAM-USER-001 bounds them.

Code: `identity/account/signin.go` (`Lock.LockedUntil`, `LockEvent`), `identity/account/signin_service.go`
(`SignIn.Login`), `identity/gormstore/store.go` (`SignIns.LockEvents`).

## IAM-USER-003 — Sign-in history

1. `user_signins` holds one row per event of a person: `signed_in`, `wrong_password`, `locked_out`,
   `refused_inactive`, `signed_in_as` (somebody signed in as the person: `actor_id`), `unlocked`,
   `signed_out_everywhere` and `session_revoked` (`actor_id` is who, empty when it was the person). The values
   are stored and reach the client, which names them. IP (45 characters) and User-Agent (255) are kept; longer
   values are cut.
2. **Last sign-in** is the latest `signed_in`.
3. The table is arv-next's as it is (columns `user_id`, `event`, `ip`, `user_agent`, `actor_id`), so an
   application that already has it adopts the module's migration with `MarkApplied`.

Code: `identity/account/signin.go` (`SignInEntry`, `Event`), `identity/account/signin_service.go` (`appendEntry`),
`identity/migrations/20261016000001_identity_signins.sql`.

## IAM-USER-004 — Sessions

1. A person's sessions are their live `tokens` rows (not revoked, not expired), the latest used first, with the
   device ("Browser · OS" read off the User-Agent: Chrome, Safari, Edge, Firefox on macOS, Windows, iPhone, iPad,
   Android, Linux; anything else the raw string), last activity and IP, expiry, and who opened it by signing in as
   the person (`ActorID`).
2. Ending one session is `session_revoked` on the person's history, ending all is `signed_out_everywhere`. The
   session the request came with is refused from a list (`400 identity.session.current`): that is signing out.
3. Ending a session revokes its row, so its token stops working at once (there is no verify cache).
4. **Ending all of a person's sessions also ends the sessions they opened by signing in as somebody else.** A
   sign-in-as session belongs to the *target* (`user_id` is the target, `name` is `login-as:<actor id>|<device>`),
   so it is not among the actor's own rows. `RevokeSessions` revokes the live sessions the person opened, and
   writes `session_revoked` (actor = who ended them) on each **target's** history. A person who is gone or has a
   new password cannot leave a way in as somebody else open.
5. **Except the caller's own.** `RevokeSessions` keeps the session whose access token is `KeepToken` (somebody who
   changes their own password stays signed in where they did it).
6. A refresh swaps the tokens in one atomic step: of two requests with one refresh token exactly one succeeds, and
   a session opened by signing in as somebody stays marked as one.

Code: `identity/account/users.go` (`Users.Sessions`, `RevokeSession`, `RevokeSessions`), `identity/account/session.go`
(`DeviceLabel`), `identity/gormstore/store.go` (`Sessions.Rotate`, `End`, `EndOpenedBy`).
