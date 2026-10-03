# Logic IDs: IAM-PROFILE-001, IAM-PROFILE-002, IAM-PROFILE-003, IAM-PROFILE-004

Title: A signed-in person maintains their own account

Status: Approved (the generic parts of arv-next, `documentation/business-rules/iam/profile.md`, IAM-PROFILE-001 to 004)
Last updated: 2026-10-03
Module: `identity` (`identity/account` `Profile`, `identity/http` routes under `/v1/iam/profile`)

## Summary

A person changes their own name and phone, their password and their e-mail address, and sees and ends their own
sessions and sign-in history. The address changes only after a one-time code mailed to the new address
([IAM-OTP-*](codes.md)); the password is asked again first, under the lock of [IAM-REAUTH-001](reauth.md); a new
password signs the person out everywhere else. What stays an application's: contact fields beyond the phone, the
legacy password rules of arv-next (`identity.WithPasswordPolicy`), the roles on its profile screen (IAM-PROFILE-006),
and arv-next's preferences blob (`/auth/preferences`), which is arv-next's onboarding state.

## IAM-PROFILE-001 — Self only, and the field rules

1. **Self only, no permission.** Every route acts on the account the session is for; no route carries an account
   id. Being signed in is all there is to authorize.
2. **Details** (`PUT /v1/iam/profile`): the name is required (at most 150 characters), the phone optional (30), no
   format check. Values are trimmed.
3. **Targeted writes.** A save writes the name and the phone only, never the whole row, so a stale page cannot
   overwrite what an administrator changed meanwhile; what changed is on the account's history (IAM-USER-007,
   action `profile`). Saving what is already there writes and records nothing.
4. The address is normalised (trimmed, lower-case) and must be one address without a display name, at most 255
   characters.

## IAM-PROFILE-002 — The login is read-only for the person

The profile shows the login and does not accept it. Only an administrator changes it ([users.md](users.md)).

## IAM-PROFILE-003 — Password change

1. Requires the **current password**; a wrong one is the field error `current_password`
   (`identity.password.wrong`). After 5 consecutive attempts without the right one, re-confirmation locks for 15
   minutes ([IAM-REAUTH-001](reauth.md), `identity.reauth.locked`).
2. The new password must satisfy the `PasswordPolicy` (`identity.password.weak`, `params.rules` names every rule
   it breaks; default: at least 8 characters, at most 72 bytes), the confirmation must match
   (`identity.password.mismatch` on `new_password_confirmation`) and it must differ from the current password
   (`identity.password.unchanged`). Passwords are never trimmed: white space is a legal character.
3. In **one transaction**: the history row (`password`, by name, never by value), the new hash, and **every other
   session ends** (the one that made the change stays, named by the access token it came with), including the
   sessions the person opened by signing in as somebody else (IAM-USER-004). A change that fails anywhere leaves
   the old password and every session as they were.
4. `PasswordChanged` is published to `Notices` after commit; with a mail sender, identity mails the account's
   address a notice (best effort: a failed mail is logged and does not undo the change).

## IAM-PROFILE-004 — The address changes by code

1. **Request** (`POST /v1/iam/profile/email`) re-confirms the current password (the same lock), then checks the new
   address: valid, not the current one (`identity.email.unchanged`) and not another account's (`409
   identity.email.taken`; **one account per address**, compared without case). A 6-digit code ([IAM-OTP-001](codes.md))
   is mailed **to the new address**, purpose `email_change`, target the new address. Nothing about the account
   changes yet.
2. **Pending.** While the code is live, `GET /v1/iam/profile` carries `pending_email` (the new address, expiry, when a
   resend is possible, attempts left), so a reload does not lose the flow.
3. **Resend** (`POST /v1/iam/profile/email/resend`) mails a fresh code to the **stored** target, never to an address
   from the request, under the cooldown and the cap of [IAM-OTP-003](codes.md).
4. **Confirm** (`POST /v1/iam/profile/email/confirm`) verifies the code ([IAM-OTP-002](codes.md); a pasted "123 456" works)
   and then checks **again** that the address is free: it may have been taken since the request. If it was, the
   change is refused with `identity.email.taken`; the code is used up and the person starts over.
5. On success the address is written, the change is on the history (before and after), `EmailChanged` is
   published to `Notices`, and with a mail sender the **previous** address is told, the new one masked
   (`a***@example.com`).
6. **Cancel** (`DELETE /v1/iam/profile/email`) ends the live code.
7. An application that runs **without a mail sender** (`identity.WithoutMail()`) cannot change addresses by code:
   request, resend and confirm answer `400 identity.email.disabled`; the rest of the profile works.

Code: `identity/account/profile.go` (`Profile.UpdateDetails`, `ChangePassword`, `RequestEmailChange`,
`ResendEmailChange`, `ConfirmEmailChange`, `CancelEmailChange`).
