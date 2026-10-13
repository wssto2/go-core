# Changelog

## v1.8.0

### Added

- `notification`: web push, opt-in with `notification.WithPush(push.Config{PublicKey, PrivateKey, Subject})` (NOTIF-DEVICE-001).
  - People register their browsers as devices (`Notices.Devices`; routes `GET /v1/notifications/push`, `GET|POST /devices`, `POST /devices/unsubscribe`, `PATCH|DELETE /devices/:id`, `POST /devices/:id/test`). Subscribing an endpoint again is one refreshed device; an endpoint another person registers moves to them and cancels the earlier person's queued pushes. New table `notification_devices` (migration `20261016000033`).
  - Pushes are delivered per active device by a worker (`notification.push`, `Notices.DeliverPushDue`). A category's priority sets Urgency and TTL (`Kind.HighPriority()`: high, 4 h; otherwise normal, 24 h). Retries on 401/403/429/5xx and no answer, honouring `Retry-After`; 404/410 removes the device; 5 failures of the device's own fault disable it; bursts of 3 (2 after quiet hours) are bundled (`notification.PushBundleText`); pushes made in quiet hours are held.
  - Push is a settings channel: `Kind.PushByDefault()`, `Settings.SetPush`, `PUT /v1/notifications/preferences/:category/push`, and `push_available` plus a `push {enabled, source}` per category in the preferences. The `system.test` notification is pushed to all devices at once; the device test route pushes to one.
  - The service worker reads the payload keys `notification_id` (0 for a bundle), `category`, `title`, `body`, `link`, `tag`, `unread_count`.
- `notification/push`: Web Push on the standard library only: RFC 8291 aes128gcm, proven against the RFC's test vector, and VAPID ES256 (RFC 8292). Its client never connects to loopback, private or link-local addresses. `notification/push/pushtest` is a fake push service with decrypting devices for tests.
- `identity.WithoutUserAdmin()`: no `/v1/iam/users` routes and no `Searcher` required, for an application with its own users module; `users.Admin()` is nil. The profile, a person's own sign-ins and sessions, login-as and its return stay.
- `identity.WithAuditRecord(entityType, actions)` and `gormstore.AuditRecord`: the history of accounts is read and written under the host's audit record type and action names. `identity.ChangeAction` and the `identity.Change*` constants are exported. Where several actions share one host verb, a row reads back as the first of them.
- `identity.WithLocales(...)` and `account.Config.Locales`: a locale outside the list is refused with 422 `identity.locale.unsupported` (change-locale, admin create and update).
- `migrations.WithoutAccounts()` and `gormstore.MigrateWithoutAccounts`.
- `access.WithRoleCheck`, `access.WithBindingCheck`, `access.WithSubjectReach` and `access.WithCache`, also as the `admin.Config` fields `RoleCheck`, `BindingCheck` and `Reach`. A role-check problem refuses the save as `authz.invalid` (400). The binding check guards bind, unbind and role replacement. A person out of reach answers 404 on access, scopes, bind and unbind.
- `route.Bound.Excuses(routes...)`: routes of a contract an application leaves out on purpose are not reported by `route.Unhandled`, and nothing is mounted for them.
- `event.Housekeeper.KeepDeadLetters(d)`: dead letters older than `d` are deleted, with the events only they kept. `gocore.WithDeadLetterRetention(d)` sets it on the housekeeper `App.Events` adds. The default still keeps them for ever.

### Changed

- **Breaking:** `notification.Enforcer` takes the channel: `func(ctx, userID, kind, channel) (on, enforced bool, err error)`. `Enforce` covers e-mail and push (`notification.ChannelEmail`, `ChannelPush`).
- Deliveries carry their channel. The e-mail worker claims only e-mail rows, and the push worker only push rows.
- `identity.WithAccounts` no longer registers the accounts, phone and unique-email migrations.
- A used, cancelled or ended one-time code answers `identity.code.no_pending`; `identity.code.expired` is only for a code past its time.
- `authz.invalid` errors carry `params.problems` as objects `{code, permission?, detail?}`, sorted by code then permission, instead of a sorted list of code strings.
- `GET /v1/iam/roles/:ref/compare` compares any two roles the actor may view. A predefined first role used to answer 403.

### Verified (pinned by tests, no change)

- Identity's authentication applies only to its own routes, so a host's own chain over the same `tokens` table coexists with it.
- Login-as and its return work from sessions marked `login-as:<actor>|<device>`.
- Login-as on an inactive person answers 400 `identity.account.inactive`.

## v1.7.2

### Fixed

- `pdf`: every document registers its own copy of the `FontSet` bytes. gofpdf writes into the font bytes while subsetting them, so documents rendered concurrently from one shared (usually `go:embed`'ed) `FontSet` raced and could corrupt each other's output.

## v1.7.1

### Security

- OpenTelemetry exporters and SDK v1.44.0 → v1.46.0, in line with `otel` itself: GO-2026-6505 (exporter config logging may leak endpoint URLs in info logs), fixed in v1.45.0.

## v1.7.0

### Added

- `notification`: e-mail, a person's preferences and quiet hours (P6).
  - E-mail goes where identity's mail goes (`identity.Users.Mail()`); with `identity.WithoutMail()` it is unavailable, never "sent". With mail, `notification.AppURL(...)` is required and `Check` says so.
  - `Category(code).EmailByDefault()`; options `AppURL`, `TimeZone` (default `time.Local`) and `Enforce`.
  - Preferences: enforced, then the person's choice, then the default (`Settings.Get`, `SetEmail`, `SetQuietHours`); quiet hours default to 21:00-07:00 in `TimeZone`, DST-correct. Routes `GET /v1/notifications/preferences`, `PUT /v1/notifications/preferences/:category`, `PUT /v1/notifications/quiet-hours`.
  - Deliveries written with the notification, a leased worker (`Notices.DeliverDue`) that retries 30 s to 1 h within 24 h and fails permanent errors at once, mail text in en, hr, bs and sl (`notification/mailtext`).
  - Housekeeping: notifications are deleted after 90 days and finished deliveries after 30 (`Notices.Sweep`).
  - Tables `notification_deliveries`, `notification_preferences`, `notification_quiet_hours` (migrations `20261016000031`, `20261016000032`), arv-next's as they are.
- `GET /v1/events/consumers` (the application's consumer names, `events.deadletter:view`) and `gocore.App.Consumers`.
- `mail.ErrPermanent`: what no retry can cure (a message the checks refuse, a recipient the relay refuses with a 5xx).
- `account.Users.Mail` and `SetMail`: where identity's mail goes.

### Changed

- `notification.Category` was a string type. It is now a function that returns a `notification.Kind`, so a category carries its defaults. `Install(app, users, TicketAssigned)` and `Send(ctx, TicketAssigned, ...)` read as before; `Install` takes `...notification.Option` (a `Kind` is one), `DedupeKey` takes a `Kind`, and `Item.Category` is a plain `string`.
- `notification.DeadLetterDeclared.To` takes the function that names the consumers (`app.Consumers`).
- `identity/account` may import `mail` (Layer 0, standard library only).
