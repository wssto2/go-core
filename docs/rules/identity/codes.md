# Logic IDs: IAM-OTP-001, IAM-OTP-002, IAM-OTP-003, IAM-OTP-004

Title: One-time codes mailed to prove a mailbox

Status: Approved (the rules moved from arv-next, `documentation/business-rules/iam/verification_codes.md`, IAM-OTP-001 to 004)
Last updated: 2026-10-03
Module: `identity` (`identity/account` service, `identity/gormstore` table, the mail text in `identity/mailtext`)

## Summary

A short numeric code, mailed to an address, proves a person controls that mailbox before an action that depends on
it takes effect. The mechanism does not know the action: the caller names a **purpose** and a **target** and decides
what happens once the code is verified. The e-mail change of the profile uses it (`email_change`, target = the new
address); `password_reset` is reserved. The numbers are defaults of `account.CodeRules`; the rules are not switchable.

## IAM-OTP-001 — Generation and storage

1. A code is **6 digits**, drawn uniformly from `crypto/rand`.
2. The plain code is **never stored**. The stored `code_hash` is the hex HMAC-SHA256 of `accountID|purpose|code`,
   keyed by the application's secret (`identity.WithCodeSecret`, at least 32 characters, required whenever mail is
   on). Binding account and purpose into the hash means a code for one purpose, or for one account, never satisfies
   another.
3. A code carries a **target**, what it confirms (the new address for `email_change`).
4. The code is returned by no route; it travels only by mail.

## IAM-OTP-002 — Expiry, attempts and single use

1. A code is valid for **15 minutes** from sending (`CodeRules.TTL`).
2. A wrong guess costs one attempt (`identity.code.mismatch`, `params.attempts_left`); the **5th** wrong guess ends
   the code (`identity.code.too_many_attempts`).
3. A successful verification **consumes** the code; it cannot be used twice.
4. A consumed, ended or expired code answers `identity.code.expired`; no code ever issued for the account and purpose
   answers `identity.code.no_pending`. Either way the person asks for a new one.
5. The limits hold under parallel requests: an outcome is saved only over the state the code was read in (a
   conditional UPDATE on `id`, `attempts`, unconsumed, not invalidated); a request that lost the race reads again.
   Parallel wrong guesses each cost an attempt, and **of several verifications of one code exactly one wins**
   (conformance test `codes/consumed once`, on SQLite, MySQL and MariaDB).

## IAM-OTP-003 — Resend cooldown and send cap

1. A new code for the same account and purpose may be sent only **60 seconds** after the previous one
   (`identity.code.resend_too_soon`, `params.retry_after` in seconds).
2. At most **5 codes per account and purpose in a rolling hour** (`identity.code.too_many_requests`,
   `params.retry_after` is when the oldest leaves the hour), so the resend button cannot flood a mailbox.
3. Resend always issues a **fresh** code, never re-sends the old one.
4. If the mail cannot be sent, the new code is ended at once (`identity.code.not_delivered`, `500`): a code nobody
   received is never live. It still counts toward the cooldown.

## IAM-OTP-004 — One live code per account and purpose

Issuing a code ends any live code of the same account and purpose **in the same transaction** (`CodeStore.Issue`).
MySQL has no partial unique index, so the service and the store enforce it.

## Differences from arv-next

- The reasons are `identity.code.*` (arv-next: `verification.*`); the client's translations are keyed by them.
- The code hash, the limits and the table are the same; the table `user_verification_codes` is the same DDL, so an
  application that has it adopts the migration with `MarkApplied`. The column `user_id` still names the account.

Code: `identity/account/codes.go` (`Codes.Issue`, `Verify`, `Cancel`, `Pending`), `identity/gormstore/codes.go`,
`identity/migrations/20261016000003_identity_verification_codes.sql`.
