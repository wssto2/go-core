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
- IAM-USER-003 — every sign-in event is on the person's history, in the views all and failed with their counts — `identity/account/signin.go` (`SignInEntry`), `identity/account/history.go` (`SignInView`), `identity/gormstore/store.go` — rules: `docs/rules/identity/signin.md`
- IAM-USER-004 — sessions are listed and ended, and ending all ends what the person opened as somebody else — `identity/account/users.go`, `identity/gormstore/store.go` — rules: `docs/rules/identity/signin.md`
- IAM-USER-008 — the payload names who is really signed in, and returning to one's own account needs no password and only an impersonation session — `identity/account/signin_service.go` (`SignIn.Return`) — rules: `docs/rules/identity/signin.md`
- IAM-USER-005 — deactivation: nobody deactivates themselves, the application's hooks can refuse in the same transaction, the sessions end — `identity/account/admin.go` (`Admin.Deactivate`) — rules: `docs/rules/identity/users.md`
- IAM-USER-006 — the list: views by status and lock with their counts, search, last sign-in and lock on each row — `identity/account/admin.go` (`Admin.List`), `identity/gormstore/store.go` — rules: `docs/rules/identity/users.md`
- IDENTITY-ADMIN-002 — a person's activity: the audit rows they wrote, by the areas the application names and by days, marked with who was signed in as them — `identity/account/activity.go` (`Admin.Activity`), `identity/gormstore/activity.go` — rules: `docs/rules/identity/users.md`
- IAM-USER-007 — the change history, names and non-secret values, from the audit trail, in the views all, access and details with their counts, each row naming who acted — `identity/account/admin.go` (`Admin.Changes`), `identity/account/history.go` (`ChangeView`), `identity/gormstore/changelog.go` — rules: `docs/rules/identity/users.md`
- IDENTITY-ADMIN-001 — create, update and a new password: one account per address, a policy for passwords, the secret never recorded — `identity/account/admin.go` (`Admin.Create`, `Update`, `SetPassword`) — rules: `docs/rules/identity/users.md`
- IAM-PROFILE-001 — a person edits only their own name and phone, by targeted writes — `identity/account/profile.go` (`Profile.UpdateDetails`) — rules: `docs/rules/identity/profile.md`
- IAM-PROFILE-002 — the login is read-only for the person — `identity/account/profile.go` (`ProfileDetails`) — rules: `docs/rules/identity/profile.md`
- IAM-PROFILE-003 — a password change re-confirms the old one, follows the policy, and signs out every other session in one transaction — `identity/account/profile.go` (`Profile.ChangePassword`) — rules: `docs/rules/identity/profile.md`
- IAM-PROFILE-004 — the address changes by a code mailed to the new address, and is checked free again at confirmation — `identity/account/profile.go` (`Profile.RequestEmailChange`, `ConfirmEmailChange`) — rules: `docs/rules/identity/profile.md`
- IAM-OTP-001 — a code is six random digits, stored only as an HMAC bound to account and purpose, and travels only by mail — `identity/account/codes.go` (`Codes.Issue`) — rules: `docs/rules/identity/codes.md`
- IAM-OTP-002 — fifteen minutes, five attempts, single use, and exactly one winner among parallel verifications — `identity/account/codes.go` (`Codes.Verify`), `identity/gormstore/codes.go` (`SaveVerification`) — rules: `docs/rules/identity/codes.md`
- IAM-OTP-003 — sixty seconds between sends, five an hour, a fresh code each time, an undelivered code is ended — `identity/account/codes.go` (`Codes.Issue`) — rules: `docs/rules/identity/codes.md`
- IAM-OTP-004 — one live code per account and purpose, in one transaction — `identity/gormstore/codes.go` (`Codes.Issue`) — rules: `docs/rules/identity/codes.md`
- IAM-REAUTH-001 — re-confirming the password locks after five wrong ones for fifteen minutes, counted before the check — `identity/account/reauth.go` (`Reauth.Confirm`), `identity/gormstore/reauth.go` — rules: `docs/rules/identity/reauth.md`

## access

- IAM-AUTHZ-005 — roles and bindings are edited through the delegation rules — `access/admin/roles.go`, `access/admin/bindings.go`, `authz/admin.go` — rules: `docs/rules/access/authorization.md`
- ACCESS-ADMIN-001 — the module asks the application for names; an unknown subject is not found — `access/admin/admin.go` (`requireSubject`), `access/access.go` (`Seed`) — rules: `docs/rules/access/authorization.md`

## notification

- NOTIF-EVENT-001 — a notification is made from an event, deduped on the event id, retried, then a dead letter; dead letters are listed and retried over HTTP; processed events are deleted after 30 days, dead letters kept — `notification/notification.go` (`Notices.Send`, `DedupeKey`), `notification/inbox.go` (`store.insertNew`), `notification/deadletters.go`, `event/housekeeping.go` — rules: `docs/rules/notification/notifications.md`
- NOTIF-CATEGORY-001 — categories are values registered with Install; a bad, repeated or unregistered one names the fix — `notification/notification.go` (`Category`, `categoryProblems`) — rules: `docs/rules/notification/notifications.md`
- NOTIF-RECIPIENT-001 — invalid ids, duplicates, inactive people and the actor are dropped, in one lookup per Send — `notification/notification.go` (`Recipients.resolve`, `Notices.Send`) — rules: `docs/rules/notification/notifications.md`
- NOTIF-CONTENT-001 — rendered per recipient, the title required, text cut to its columns, the link an in-app path — `notification/notification.go` (`finish`, `IsAppLink`) — rules: `docs/rules/notification/notifications.md`
- NOTIF-READ-001 — read state per notification, mark all only up to what was seen, the stream tells every open app after commit — `notification/inbox.go` (`Inbox.MarkRead`, `Inbox.MarkAllRead`), `notification/hub.go`, `notification/stream.go`, `notification/housekeeping.go` (`Notices.Sweep`) — rules: `docs/rules/notification/notifications.md`
- NOTIF-PREF-001 — the e-mail of a category is what Enforce says, else the person's own choice, else the category's default; only differing choices are stored; no mail, no e-mail — `notification/settings.go` (`Settings.resolve`, `Settings.SetEmail`), `notification/settings_store.go` — rules: `docs/rules/notification/notifications.md`
- NOTIF-QUIET-001 — default 21:00 to 07:00 in the application's zone, DST-correct; an e-mail made in them is held until they end, and sent only if still unread — `notification/quiet.go` (`QuietHours.ReleaseAt`), `notification/delivery.go` (`Notices.emailPlans`) — rules: `docs/rules/notification/notifications.md`
- NOTIF-DELIVERY-001 — an e-mail delivery is written with the notification, sent once under a lease, retried 30 s to 1 h within 24 h, failed at once on a permanent error — `notification/delivery.go` (`Notices.DeliverDue`, `deliveryRow.record`), `notification/delivery_store.go` (`store.claimDue`), `notification/delivery_worker.go` — rules: `docs/rules/notification/notifications.md`
