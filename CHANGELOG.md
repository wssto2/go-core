# Changelog

## Unreleased (after v1.7.0-rc.1)

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
