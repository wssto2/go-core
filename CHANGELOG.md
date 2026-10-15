# Changelog

## v1.10.0

Every route says who may call it. A route that declared nothing was already served to any authenticated caller; now that choice is written down, and a forgotten permission is a start-up error instead of an open route.

### Added

- `route.Authenticated()` on `Route` and `RawRoute`, and `Spec.Authenticated`: any authenticated principal may call the route and the handler decides the rest. It cannot also be `Public`. `Spec.DeclaresAccess()` says whether a route names a permission or is `Public`, `PersonOnly` or `Authenticated`.
- `gocore.App.Served()`: the installed routes as `[]route.Served` (the `Spec` and the path served, prefix or site root included), in mount order.
- `gocore.App.Contracts()` and `route.Contracts(handled...)`: the groups of the installed routes, each once, in order, for `contract.Generate`.
- `contract.Reachable(groups...)`: the input, output and enum types `contract.Generate` would write for the groups, from the same walk, so an application lists no types beside its routes.
- `migrate mark-applied <connection> <version|file>...` in `RunCommand`, and `gocore.App.MarkApplied(ctx, conn, versions...)` behind it: records migrations as applied without running them (a database whose tables already exist), over `Migrator.MarkApplied`. `migrate.Version` reads the number or the file name.
- `archtest`: the architecture rules of a layered go-core application, checked from source with `go/parser` (no new dependency). `archtest.Run(t, archtest.Config{...})` takes the layers (domain, application, infrastructure, interfaces, cmd), a ratchet per rule (`Allowed`: a new violation and a stale entry both fail), the application's own bans (`Banned{Import, Symbol, Why}`) and the few inputs a rule needs (shared domains, owned domains, the input and service naming). The rules are: no panic, `context.Background`, `time.Sleep`, `database/sql` or `time.Now` where they do not belong; `fmt.Errorf` wraps with `%w`; handlers do not import the persistence package; layers import inward only; a framework-free domain and application; no cross-aggregate domain imports; `context.Context` first on domain ports and application service methods; every string input has a `max:` bound; route paths are kebab-case and group and permission names single words; every `validation` rule is known; every `route.Get/Post/Put/Patch/Delete/Raw` chain declares who may call it (`Requires`, `RequiresAny`, `RequiresAll`, `Public`, `PersonOnly` or `Authenticated`); a service injects only its own aggregate's repositories; the repository shape (the transaction and tenant-scope plumbing in one file). `archtest.Check` returns the results instead of failing a test, and `archtest.Sources` and `archtest.Ratchet` give an application's own guards the same parsed files and the same ratchet.
- go-core holds its own `ARCHITECTURE.md` section 4 with it (`archtest/dogfood_test.go`): the layers, the per-package prohibitions, `gocoretest` imported by tests only and the framework-free `identity/account`. This replaces `identity/account`'s own imports test.

### Changed

- **Breaking:** `App.Check` (and so `Run`) refuses a route, typed, `Raw`, `Root` or `RoutesWith`, that declares none of `Requires`, `RequiresAny`, `RequiresAll`, `Public`, `PersonOnly` or `Authenticated`. The problem names the method, the served path and the choices. Add `.Authenticated()` to a route that was meant for any signed-in caller: behaviour is unchanged.
- `access.bindable` (`GET /v1/iam/bindable-roles`) and `access.me` (`GET /v1/me/access`) declare `.Authenticated()`.

## v1.9.0

One authentication chain: go-core identity authenticates every route, whether the caller signs in with a session, calls with a service account's API key, or arrives through an embed. An application keeps no auth plumbing of its own, and site-wide middleware and telemetry need no container.

### Added

- `gocore.App.Root(routes...)`: routes served outside the prefix (`/sw.js`, `/embed`), behind the app's security unless `Public`.
- `gocore.App.Telemetry()`: the application's one metrics handle. The builder (`AppBuilder.WithTelemetry`) and worker metrics reuse it instead of building their own.
- `gocore.App.Use(middleware...)` and `AppBuilder.Use`: engine middleware that runs before every route, those of modules included, in the order given, however late it is added.
- `gocore.WithCSP(directive, sources...)`, `AppBuilder.WithCSP` and `middlewares.CSPSources`: add sources to a CSP directive, creating it when missing.
- `gocore.App.OnStart(check)`: a check serving runs before it starts, once the migrations are applied (a config row the app cannot run without, data it reads once per process). An error stops start-up with the other problems; commands and `Check` do not run it.
- `gocore.WithServiceWorker(file)`: serves the file at `/sw.js`, outside the prefix, public, with `Cache-Control: no-cache`.
- `identity.WithRequestUser(func(ctx, who identity.Authenticated) (auth.Identifiable, error))`: the application's own user for every request. It runs for a signed-in account and for an API-key request; `identity.Authenticated` carries either the account (with its session) or `ServiceAccount`.
- `route.PersonOnly()` on `Route` and `RawRoute`: a service account gets 403 (`authz.forbidden`, `why: person_only`). Identity's session and profile routes and every notification route (inbox, settings, devices, live stream, test notification, failed events) are person-only.
- `identity.WithServiceAccounts(serviceaccount.Config{...})`, with these parts:
  - the packages `identity/serviceaccount` (with `gormstore`, `migrations`, `storetest` and `http`) and `identity/serviceaccount/keynotice`;
  - the tables `service_accounts` (with `tenant_id` and `owner_id`), `service_account_keys` and `service_account_key_notices`;
  - the routes under `/v1/iam/service-accounts`: list, show, create, update, deactivate, keys, create a key (the secret shown once, `Cache-Control: no-store`) and revoke one;
  - `X-API-Key` authentication on identity's chain, as `authz.ServiceAccount`; keys are stored as SHA-256, and `last_used_at` is stamped at most once a minute;
  - an `audit_logs` row for every request made with a key that is not a read;
  - a daily worker that tells a key's owner once when it expires within 14 days or has been unused for 60 days (`keynotice.Install`, categories `iam.serviceaccount.keyexpiring` and `iam.serviceaccount.keyunused`);
  - access routes for service accounts, mounted by `access.Install`;
  - the permissions `iam.serviceaccount:view`, `:manage` and `:revoke`, defined by `identity.DefinePermissions`.
- `access/admin`: a service account's access is guarded by `iam.serviceaccount:view` and `:manage`, a person's still by `iam.user:*`. New `Bindings.BindableRolesFor(ctx, kind, scope)` and the constants `ViewServiceAccountAccess` and `ManageServiceAccountBindings`.
- `identity.WithEmbed(secret)`: `GET /embed` at the site root. A signed, single-use token opens a 24-hour session with no refresh, for an application shown in another host's iframe. The `redirect` must be a path of this site; any other value becomes `/`. The secret must be at least 32 characters. `account.SignIn.Embed` is the sign-in behind it.
- `auth.EnforceUserRateLimit`.
- `cmd/modulets` writes `serviceaccount/` (the service-account routes, their keys and their access) next to the other module folders.

### Changed

- The development CSP allows `'unsafe-eval'` (vue-core's Zod compiles with `new Function`); production stays nonce-only.

### Fixed

- The per-user rate limit armed by `WithRateLimit` now applies on identity's authentication chain. Before, identity, access and notification routes were not limited per user.

## v1.8.8

Typed routes set response headers and combine permission rules, so an application keeps neither as `route.Raw` nor as a check in its handler.

### Added

- `route.Exchange.SetHeader(name, value) error`: a typed handler sets a response header (for example `Cache-Control`) on success and on error. `Content-Type` and `Content-Length` are the envelope's and return `route.ErrReservedHeader`.

### Changed

- `Requires`, `RequiresAny` and `RequiresAll` combine on one route, and all must hold: `Requires(a).RequiresAny(b, c)` is a and (b or c). A refusal names the first unmet `Requires`, else the any-of's last id (as `RequiresAny` alone does), else the first unmet all-of id. Before, mixing them was refused at mount.

## v1.8.7

Generated route tables carry the envelope's `meta` type. Needs vue-core 0.5.4 or later.

### Changed

- `contract.Generate`: a `route.Page[Row]` output generates `route<In, Row[], PageMeta>`, with `PageMeta` imported from `@wssto2/vue-core/client`; a `route.WithMeta[T, M]` output generates `route<In, T, M>`, with `M` imported from `./entities`. Other routes keep two type arguments.

## v1.8.6

Typed routes answer everything `web` answers, so an application's features need no `route.Raw` for creates, paged lists, filters or several permissions.

### Added

- `Route.Status(2xx)`: a typed route's success status (201 for creates). A 204 answers no body. Anything outside 2xx panics at declaration. Recorded in `Spec.Status`.
- `route.Page[T]` answers exactly what `web.Paginated` writes (`data` plus the paging `meta`, with `Result.Meta` merged), and `route.WithMeta[T, M]` exactly what `web.JSON(c, status, data, meta)` writes. Both implement `route.Enveloped` and combine with `Status`. `web.PageMeta` is the meta builder both use. A plain `datatable.DatatableResult` output keeps its nested form.
- A `query:"*"` field of type `url.Values` (at most one per input) takes the query parameters no other field declares. Recorded in `Spec.FreeFormQuery`. `datatable.ParamsFromValues(url.Values)` gives the params `ParamsFromGin` gives (which now calls it).
- `RequiresAny(...)` and `RequiresAll(...)` on `Route` and `RawRoute`, recorded in `Spec.AnyOf`/`Spec.AllOf`; `Spec.Needs()` lists every permission a route names. Start-up refuses an undefined id, mixing the forms, or `Public()` with a permission.
- `route.Exchange.RequestID()`: the request-id middleware's id, else the incoming `X-Request-ID`.
- `auth.UserOf[T](ctx)`: the application's own user, as its authentication put it with `auth.SetUser`, in a typed handler.
- `validation.CheckRules(values or reflect.Types...)` finds validation rules the validator does not know. Routes run it when the handler is bound, so an input with `gte=0` fails at start-up (and in `gocore.Check`) instead of answering 500 on every request.
- README: the "moving a module to `Install`" checklist covers 201/204, paged lists, filters, several permissions, the request id and the user.

### Generated TypeScript

- `Page[T]` generates `T[]` and `WithMeta[T, M]` generates `T` (the client unwraps the envelope); `M` is exported as an entity, and meta stays untyped on the client. The status and the permission rules are not in the generated TypeScript.
- `routes.ts` no longer imports a `WithMeta` meta entity it does not use (it is still exported from `entities.ts`), so `noUnusedLocals` holds.
- A `query:"*"` field never becomes a schema or entity property, whatever its `json` tag (it used to generate `*: z.unknown()`).
- A route with free-form filters types its input as `In & Record<string, QueryValue>` (vue-core's `QueryValue`).
- A request type whose body/query schema has no field (only path fields and/or free-form filters) is left out of the route input, which is then `{ id: number } & Record<string, QueryValue>`; its empty schema is not emitted. It used to infer `Record<string, never>` and collapse the whole input to `never`.

## v1.8.5

An application moves its own features to `Install` one at a time, behind its own security, with transport-only request types.

### Added

- `App.RoutesWith(route.Security, routes...)`: mounts an application's routes behind a chain it chooses (for example API keys, an embed token and its own rate limit), next to identity's. The authorizer defaults to the App's; public routes stay public, `Requires` is still checked, start-up still refuses an undefined permission, and the modules' routes keep identity's chain.
- `gocore.FromContainer[T](app) *Deferred[T]`: an `Install` feature takes a collaborator the old `App.Modules` still bind. It is read from the container after the old modules register and before routes serve; start-up fails with the type's name if it was never bound. For the migration only: hoist what can be built from `app.Database()`.
- `accesshttp.ScopeOf`, `RoleSummaryOf`, `RoleOf`, `BindingOf`, `SubjectAccessOf`: the builders the access routes answer with, so an application serving its own subject kind answers in the same wire.
- `route.Group(...).MalformedPath(http.StatusBadRequest)` and `Route.MalformedPath(...)`: a path parameter that does not parse (an `int` id given `abc`) answers 400 `invalid <param>` in go-core's envelope instead of 422 `must_be_number`. 422 stays the default; the route's setting wins over its group's; only 400 and 422 are accepted. Query, body and validation errors stay 422.
- README: "moving a module to `Install`" checklist.

### Changed

- `route`: the binder fills `path:` and `query:` fields, and the JSON body, through embedded structs, so `struct{ ThingPath; app.CreateInput }` is a request type. A 422 names the promoted field as before, and a body `id` cannot overwrite a path field.
- `contract` / `cmd/modulets`: `path:` fields render in the route's input (the URL parameters), also when they are `json:"-"` or inside an embedded struct, and never in the body schema; embedded `query:` fields render as optional.
- `contract` / go2ts: an untagged embedded struct renders flat, as `encoding/json` encodes it. **Breaking (generated TypeScript):** an exported embedded struct no longer produces a nested key named after its type.

## v1.8.4

`v1.8.3` is retracted: it was tagged on v1.8.2's commit and carries none of this. Use v1.8.4.

Module routes see the signed-in person's tenant, so an application's hooks read tenant-scoped data as the person does.

### Added

- `identity.WithTenant(func(ctx, account.Account) (tenantID int, all bool, err error))`: the application says which tenant a signed-in account belongs to (or that it sees all of them), and identity puts it on every route, the modules' and the application's, right after authentication (`tenancy.WithTenantID` / `WithAllTenants`; an error aborts the request). Without it no tenant is set, as before. identity's accounts are not `TenantAware`, so this is how an application that signs in through identity gives its hooks (`access.WithSubjectReach`, `WithBindingCheck`) the tenant.
- `notification.TestMessage(func(Recipient) Message)`: the application words the test notification (`POST /v1/notifications/test` and a device's test push) per recipient, with its own link. Default: the English text, no link.
- `web.PathInt(ctx, key) (int, bool)`: a path id that is not a positive integer answers 422 with the standard validation body (`must_be_number` / `must_be_positive_number` on the parameter), as a route-bound input does, and aborts; the handler returns on false.

### Changed

- App routes run `tenancy.FromAuthenticatedUser` after authentication, for applications whose authenticated users are `TenantAware` (`gocore.WithAuthentication`).
- `access`: `GET /v1/iam/users/:id/scopes` offers only the places where the `WithBindingCheck` accepts a binding of that subject (asked once per place and for the root; a 4xx refusal leaves the place out, any other error fails the request). Before, every place the actor could assign was offered and the bind was refused afterwards.
- `event`: one worker, `event.consumers`, fetches every consumer's due events in one query per tick and hands each consumer its own, which it leases and handles as before (order and batch size per consumer unchanged; no `SKIP LOCKED`). **Breaking:** `Queue.Workers()` returns that one worker instead of one per consumer.

### Deprecated

- `web.GetParamInt`: it answers 0 for a missing or malformed id without an error. Use `web.PathInt`.

## v1.8.2

A host that caches verified tokens next to identity no longer serves a signed-out token.

### Added

- `auth.DBTokenProvider.InvalidateUser(userID)` drops every cached entry of a person; `InvalidateSession(tokenID)` drops one session's. A lookup already in flight when an invalidation happens answers its own request but is not cached.
- `account.Notices.SessionsEnded(ctx, account.SessionsEnded{AccountID, SessionIDs, Reason, ActorID})`: identity says when sessions stop working, after the change is committed. `account.EndReason`: `logout`, `rotation` (a refresh replaced the access token), `revoke`, `password_changed`, `deactivated`, `login_as_return`, `opened_by_ended`.
- `account.Notices.AccountChanged(ctx, accountID)`: after a change of locale, name, phone, e-mail, password, activation or deactivation.
- Wire both to `InvalidateUser` (README, "A host that caches verified tokens"; `ExampleWithNotices_tokenCache`). Proven end to end on SQLite, MySQL and MariaDB: after logout or refresh, the old token is refused on the host's own route on the very next request.

### Changed

- **Breaking:** `account.Notices.SessionsRevoked` is replaced by `SessionsEnded`. A `Notices` that defined it changes its method; one that embeds `DiscardNotices` and does not override it needs nothing.
- The facts of a password change and a deactivation are told after their transaction commits, not inside it.

### Fixed

- A login-as refused by the application's `Impersonation` with a forbidden error that carries no reason answers `authz.forbidden` with the standard body.

## v1.8.1

### Added

- `identity.WithFieldLimits(account.Limits{Login, Name, Email, Phone})` (also `account.Config.Limits`, `identityhttp.Config.Limits`): how long the host's store keeps each field. Zero is go-core's width (100, 150, 255, 30); a host limit may only be smaller, and a larger or negative one stops `Install` (`account.ErrLimits`). A name, phone, e-mail address or login longer than the limit is refused with 422 and the same `max` field error as the input's tag, on admin create and update, profile update and e-mail change. A login longer than the limit signs in as a wrong sign-in (`identity.signin.failed`), so a stranger learns nothing. Service callers outside HTTP (`users.Admin()` from code) still meet only go-core's width.
- `account.LoginAs{ActorID, TargetID, Device, IP}`: what a login-as fact carries. `Device` is the `DeviceLabel` of the User-Agent. A login-as and its return carry the request's device and ip; a sign-out or an administrator ending the session carries those of the ended session.

### Changed

- **Breaking:** `account.Notices.SignedInAs` and `SignedOutAs` take `(ctx, account.LoginAs)` instead of `(ctx, actorID, targetID int)`. A `Notices` that defines them changes its signatures; one that embeds `DiscardNotices` and does not override them needs nothing.

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
