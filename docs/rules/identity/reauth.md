# Logic IDs: IAM-REAUTH-001

Title: Password re-confirmation locks after five wrong passwords

Status: Approved (moved from arv-next, `documentation/business-rules/iam/reauth.md`, IAM-REAUTH-001)
Last updated: 2026-10-03
Module: `identity` (`identity/account` service, `identity/gormstore` table)

## Summary

A profile asks for the current password before a password change and before an e-mail change is requested. Without
a limit, a hijacked session could guess the account password as fast as requests are allowed. Re-confirmation
therefore locks after a few wrong passwords, like a one-time code dies after five wrong guesses
([IAM-OTP-002](codes.md)). The numbers are defaults of `account.Config.ReauthLock`.

## IAM-REAUTH-001 — Re-confirmation locks after 5 wrong passwords

1. **One counter per account**, shared by every re-confirmation with the current password (the password change and
   the e-mail change request). Sign-in is not a re-confirmation and has its own lock (IAM-USER-002).
2. **Count first, check second.** Each attempt is counted before the password is checked, and a correct password
   clears the count. So the count is the number of consecutive attempts without a correct password.
3. **The 5th attempt locks** re-confirmation for **15 minutes**. That 5th password is still checked: if it is right
   the lock is lifted at once; if it is wrong the answer is already the lock.
4. **While locked, the password is not checked at all**, not even a correct one, so the lock tells an attacker
   nothing. The answer is `400`, reason `identity.reauth.locked`, params `retry_after` (seconds) and `locked_until`
   (RFC 3339).
5. **The lock ends by itself** after 15 minutes and the next attempt starts a fresh count. It is not tied to
   signing in again, and there is nothing for an administrator to reset. At most 5 passwords are checked per 15
   minutes, about 480 a day.
6. **Parallel requests each count.** A count is saved only over the counter as it was read (a conditional UPDATE on
   `failures` and `locked_until`; the first attempt inserts the row, and a second first attempt that loses the
   insert reads it again). A burst of parallel guesses gets exactly 5 password checks (conformance test
   `reauth/one counter wins`, on SQLite, MySQL and MariaDB).
7. A wrong password before the lock is the field error `current_password` of the form (`identity.password.wrong`).

## Differences from arv-next

- The reason is `identity.reauth.locked` (arv-next: `iam.reauth_locked`).
- The table `user_reauth_attempts` is the same DDL; `user_id` still names the account.

Code: `identity/account/reauth.go` (`Reauth.Confirm`), `identity/gormstore/reauth.go`,
`identity/migrations/20261016000004_identity_reauth_attempts.sql`.
