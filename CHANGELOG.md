# Changelog

## v1.22.0 (unreleased)

One package to start an app. `gocore` is the whole entry point: `bootstrap` is gone, `gocore.New` reads the environment, settings are read with explicit calls that stop start-up on a bad value. **Breaking**: an application upgrading from v1.21.0 follows "Removed" and "Changed".

### Removed

- The `bootstrap` package: `bootstrap.Config` (now `gocore.Config`), `bootstrap.EnvInt`, `EnvBool`, `EnvDuration`, `EnvStr`, `Component` (internal), the app builder, `gocore.WithBuilder` and `WithIPRateLimit` (use the `gocore` options below), and the `cfg.Frontend.APIPrefix`, `cfg.Storage` and `cfg.JWT` settings (the one prefix is `WithPrefix`; `API_PREFIX` is no longer read).
- `identity.WithTenant`, `tenancy.TenantOf`, `Tenant` and `HasTenant` (`TenantAware` is `GetTenantID` alone: the tenant follows the sign-in).
- identity options that did one job twice: `AllowImpersonation` (use `WithImpersonation`), `WithNavigationProvider` (use `WithNavigation`), `WithLocales` and `WithFieldLimits` (use `WithConfig(identity.Config{Locales, Limits})`), `WithCodeSecret` and `WithMailContent` (use `WithMail(identity.Mail{Sender, CodeSecret, Content})`).
- `incident.Alerts.Notices`: `incident.Install` takes the notices and registers `incident.AlertCategory` itself, so the application no longer installs notification with it.
- The `access` aliases of `access/admin` types: import the type from `access/admin` (or `authz` / `auth`); `access` installs.

### Changed

- **Breaking:** `gocore.New(opts...)` takes no config; it reads `APP_NAME` (no default now), `APP_ENV`, `APP_PORT`, the HTTP timeouts, `HTTP_MAX_BODY_BYTES`, `TRUSTED_PROXIES`, `LOG_*`, `DATABASE_LOG_*`, `I18N_*`, `STATIC_*` and, for each `gocore.WithDatabase("local")`, `LOCAL_DB_*`. `gocore.WithConfig(cfg)` replaces the environment, for tests. The first connection declared is the primary; there is no implicit `DB_*`. A malformed value stops start-up naming the variable instead of using the default (`APP_PORT=80a0`).
- **Breaking:** `gocore.DefaultConfig()` holds the values of a production app (timeouts 15/15/60/10 seconds, database log level `error`, templates `frontend/templates/*.html`).
- **Breaking:** an app's command and worker bodies receive a context already marked all-tenants (`tenancy.WithAllTenants`); narrow it with `tenancy.WithTenantID`.
- **Breaking:** `app.Command(name, short, func(ctx, cmd *gocore.Cmd) error)`: `cmd` is a `flag.FlagSet` with the name, usage and output set, plus `List`, `App` and `Out`. `-h` prints the flags and exits 0; a stray argument is an error unless `cmd.Parse(gocore.Args(n))`.
- **Breaking:** `mail.SMTPFromEnv(app, defaultFromName)` and `mail.SenderFromEnv(app, defaultFromName)` take the app (`mail.App`, which `*gocore.App` satisfies) and read through `app.Env()`. Outside `APP_ENV=production`, a configured relay (`SMTP_HOST`) needs `MAIL_REDIRECT_TO` or an explicit `MAIL_LIVE=true`, else start-up stops with one problem naming both. Production and an unconfigured relay are unchanged.
- **Breaking:** `incident.AlertsFromEnv(app)` reads through `app.Env()`: a malformed `INCIDENT_*` number or flag stops start-up instead of keeping the default.
- `App.Handler()` mounts `/health` and `/ready` like `Run`.
- Every `gocore.New` in go-core's own tests sets `WithConfig(DefaultConfig())`, so no test reads the machine's environment.
- Docs: the README "Getting started" is start an app, read a setting, add a command; doc comments no longer name removed API.

### Added

- Package `env`: `env.New(lookup)` and `app.Env()` with `Required`, `String`, `Int`, `Bool`, `Duration` and `List`; problems name the variable and are listed with the other start-up problems by `Run` and `Check`.
- `gocore` options `WithSPA`, `WithSPAConfig`, `WithRateLimit(perUser, perIP)`, `WithGlobalRateLimit`, `WithIdempotency(ttl)`, `WithTrustedOrigins(...)`, `WithPrefix`, `WithDatabase(name, DefaultMaxOpenConns(n))`; `gocore.Cmd`, `gocore.Args(n)`.
- A disk declared with `Public("/media")` (a path) is served read-only by the app, on `Run` and `Handler`: `GET` and `HEAD`, the file with its content type, 404 for a missing one, no directory listing, no `..`. An absolute public URL is someone else's and nothing is mounted. `storage.Files.PublicPath()` and `storage.DiskSpec.Name()`.
- `gocore.App.Environment()` ("production", "staging" or "development").
- `notification.Notices.Register`, `identity.Mail`, `identity.HoldersOf`, `identity.Menu`, and `types.NewNullIntPtr`, `NewNullFloatPtr`, `NewNullDatePtr`.

## v1.21.0 (2026-10-19)

Clean-up and safe defaults. The container, the old auth and event layers and other dead API are removed, and the defaults of a new app are the safe ones. **Breaking**: an application upgrading from v1.20.0 follows "Removed" and "Changed".

### Removed

- The bootstrap container and `bootstrap.Module`, `gocore.FromContainer`, `App.Modules`, `LoadConfig` and the `env:` tags, `WithModules`, `WithJWTAuth`, `WithDBTokenAuth`, `WithAuthProvider`, `WithOutboxWorker`; `worker.Pool`.
- `auth.Policy`, `Authorize*`, `AuthorizedWith`, `GeneratePolicy` and the JWT, OIDC, Multi and DB token providers, `IssueToken`, `ParseToken`, `Claims`, `TokenConfig`, `IdentityResolver`, `RotateRefreshToken`, `GenerateAPIKey`, `GenerateRefreshToken`; the gin `auth.GetUser`, `MustGetUser` and `GetIdentifiable` (use `UserOf` or `UserFromContext`); the dead `tenancy` functions.
- `event.Bus`, `InMemoryBus`, `OutboxWorker`, `InsertOutboxEvent`, `IdempotentHandler`, `InMemoryProcessedStore`, `WrapEventWithMetadata`, `FetchPending`, `MarkProcessed` (use `event.Define` and `Queue`).
- The packages `engine`, `utils` and `testhelpers`; `middlewares` `BindRequest`, `GetRequest`, `MustGetRequest`, `LoadShedding`, `GetRequestID`; `logger` `MustNew`, `GetFromContext`, `WithSource`, `WithUser`; `database.GetNextOrderNumber` and `GetNextDocumentNumberAndYear`; `route.FreeFormTag`; `go-core new module`.
- `Route.RequiresAll` and `RawRoute.RequiresAll`: chain `Requires` instead.
- `Route.MalformedPath` and `Contract.MalformedPath`: a malformed path id is always 400.

### Changed

- **Breaking:** `.Requires(a).Requires(b)` needs both permissions; before, the second call replaced the first and the check on `a` was silently lost. `Spec.AllOf` holds every `Requires` after the first.
- **Breaking:** a path id that does not parse (an `int` id given text) answers 400 `invalid id` instead of 422. Query and body errors stay 422.
- **Breaking:** `frontend.StateBuilder` (and `WithSPA`'s argument) is `func(*gin.Context) (any, error)`. An error answers the SPA shell with 503 and a plain page, and is reported like a handler error: logged, and an incident when incidents are installed. `middlewares.ErrorHandler` leaves a response a handler already wrote alone.
- **Breaking:** `database.ConnectionConfig.ConnMaxLifetime` and the same field of the bootstrap database config are a `time.Duration` (they were minutes as an `int`).
- **Breaking:** `APP_ENV` (`AppConfig.Env`) is `production`, `staging` or `development`; empty means development; any other value is a start-up problem naming it, and until fixed it gets the production behaviour. `staging` behaves exactly as `production`: the strict CSP, no debug error bodies, HSTS, log to file only. New: `AppConfig.Development()`, `AppConfig.Validate()` and the `Env*` constants.
- `App.Handler()` builds the engine the way `Run` does: the configured trusted proxies decide `ClientIP`, error bodies follow `APP_ENV`, the body limit applies; it no longer sets gin's global mode. A test proves a configured trusted proxy decides `ClientIP` through both.
- HSTS is sent behind a trusted proxy (`HTTP.TrustedProxies` set) that says `X-Forwarded-Proto: https`, not only when Go terminates TLS.
- `Registry.CloseAll` joins its errors with `errors.Join` (they stay inspectable with `errors.Is`); the "unsupported driver" error lists the supported drivers.
- `resilience` is internal to `httpclient`; `binders` is `internal/binders`; `audit.MaskJSON` and `observability.NewServiceMiddleware` are unexported; `event.ReserveKey` and `ConfirmKey` are unexported.
- `bootstrap.Component` and `WithComponents` replace `Module` and `WithModules` (gocore's own lifecycle hook).
- `go-core-example` uses `Install` with identity and access.
- `ARCHITECTURE.md` is cut to the dependency rules `archtest` checks plus a package map (its sections are 1, 1.1, 1.2 and 2); `AGENT.md`, `PLAN.md` and `tmp.md` are deleted; the README starts with a "Getting started".

### Added

- A JSON request body limit: `HTTPConfig.MaxBodyBytes` (default 4 MiB, negative for none), `Route.MaxBody(n)` and `RawRoute.MaxBody(n)` for one route, `web.BodyLimit`. A body over it answers 413 (`apperr.PayloadTooLarge`, `CodePayloadTooLarge`). Multipart uploads are not limited by it (`web/upload` has its own). The fixed 10 MiB cap that only typed routes had in the binder is replaced by this one.

## v1.20.0 (2026-10-19)

Notification metrics. Opt-in by having telemetry (every `gocore.New` app has it); nothing else changes.

### Added

- notification: Prometheus counters `go_core_notification_{created,held,sent,cancelled}_total` and `go_core_notification_failed_total{reason}` (`gone`, `rejected`, `permanent`, `expired`), by `category` and `channel` (`in_app`, `email`, `push`), on the app's telemetry (NOTIF-DELIVERY-001 rule 12); an app without telemetry counts nothing.
- observability: `Telemetry.Registerer`, the app-labelled registerer, for features that register their own collectors.

## v1.19.0 (2026-10-19)

Locks, intervals and safe file names; the incident staff can read an incident's occurrences. All additions.

### Added

- lock: named leases with an owner and a time to live (`lock.Install(app)` returns a `*Locker`; `Hold` keeps a lease with a heartbeat); a GORM store and the `locks` migration, MariaDB 10.3 safe.
- worker: `Every(name, interval, fn)`, a `Worker` that calls a function at an interval and logs its errors.
- database: `CreateUnlessDuplicate(db, value)`, an insert that reports a unique-key refusal as "not created" instead of an error.
- storage: `PutUnique` saves under a free name atomically (the name is numbered when taken), `PutNew`, and `SafeName`; a driver that cannot do it atomically is refused.
- incident: an incident's occurrences for the staff, `GET /v1/incidents/:incidentID/occurrences`; secret path parameters and error fields are redacted at capture.
- gocore: `App.Offline()` reports whether the app is offline. contract: a text-written int is a string in the route table.

## v1.18.1 (2026-10-19)

### Changed

- incident: a client (browser) error carries its `source` (how the page caught it: `vue`, `unhandledrejection`, `window`, `handled`) and its detail in the occurrence's context.

## v1.18.0 (2026-10-18)

### Added

- identity: `WithPreferences(identity.Preferences{})` keeps a JSON object of preferences per person (dark mode, the tips a person has dismissed): merged on update, answered whole, capped and validated, a route that needs a signed-in person, in `/auth/me` when installed. Table: `identity/preferences/migrations`.

## v1.17.0 (2026-10-18)

One binary: an application adds its own commands to the command line, loads its .env through go-core, and gets its TypeScript from the installed App instead of hand lists. Everything new is opt-in; an application that uses none of it starts, serves and prints help exactly as before.

### Added

- gocore: `App.Command(name, short, run)` adds an application command (`./myapp db:prune -dry-run`). It runs like `migrate`: the databases New opened are there (`app.Database`), nothing is served, no worker or OnStart runs; it parses its own arguments (a `flag.ErrHelp` is a success, so `-h` exits 0). `help` and the unknown-command error list the application's commands. An empty, spaced, `-`-prefixed, built-in or duplicate name is a start-up problem, and a failed Install stops a command.
- gocore: `LoadEnv(path)` reads a .env file into the environment with godotenv's rules (existing variables win; a missing file is `fs.ErrNotExist`). Call it first in main, before the configuration is read.
- gocore: `Offline()` makes an App that is installed and inspected but never run: Installs run, no database is opened (`App.Database` returns handles that fail on use), nothing is served, started or migrated (`Run` and `migrate` return an error), and the logger writes warnings to stderr without files. `App.Catalogues()` lists the permission catalogues handed to `Permissions`, each once.
- gocore: `WithTypeScript(TS{Entities, Inputs, Dir})` adds the built-in `ts:generate [entities|schemas|permissions|routes]`, which writes the application's route tables (`App.OwnContracts`), permission union (its catalogue), entities and schemas (`go2ts.Sync`) under `Dir` (default `frontend/generated`). `New` makes the App offline by itself when the command line asks for `ts:generate`; `WithArgs(args...)` sets the command line (default `os.Args[1:]`).
- gocore: `App.Shipping()` marks the routes a go-core module installs, and `App.OwnContracts()` lists the application's own route groups (what ts:generate and a `contract.Wire` audit walk). identity, access, notification, savedview and incident mark theirs.

### Changed

- `ts:generate` is a reserved command name for `App.Command`; `help` lists it only for an application that uses `WithTypeScript`.

## v1.16.1 (2026-10-18)

### Fixed

- go2ts: a type encoding/json writes as text (MarshalText without MarshalJSON) was written by its kind: `authz.Qualifier` came out as `number`. It is now `string` (Zod `z.string()`, no non-zero refine), or its declared enum; a map key with MarshalText is `string`. The types go2ts maps by name (time.Time, database/types) are unchanged.

### Added

- route: `TextEnum(values...)` declares the values of such a type from its MarshalText; go2ts and contract accept it as an enumeration. `go2ts.WritesText(t)` is exported.
- identity: its contract declares `Qualifier`, so the generated entities say `qualifier: Qualifier` ("own" | "own_location" | "all").

## v1.16.0 (2026-10-18)

The guards and generators an application kept to itself: SQL portability in Go sources, Logic-ID anchors, person-only route areas, a wire audit of the response types, the entity generator's column-type map and a rolled-back transaction for contract tests on an existing schema. Every new rule is opt-in.

### Added

- archtest: opt-in rule `sql-portability` (`Config.SQL{Dirs}`) flags SQL that MariaDB 10.3 rejects in the string literals and the gorm `clause.Locking` forms of the Go sources (RETURNING only in capitals there). The pattern table is `archtest.SQLRules()`; `dbtest.Portability` uses it.
- archtest: `LogicIDs(t, LogicIDConfig)` and `CheckLogicIDs` hold the code to a registry of Logic IDs: comment anchors in Go, text anchors in frontend files, a `NotYetPorted` ratchet, and rule headings checked against the mapping. go-core's own docs test uses it.
- archtest: opt-in rule `route-person-only` (`Config.PersonOnly{Prefix, Areas, External}`): every route in a person-only area declares `PersonOnly` or `Public`, and every area that is not `External` has a route.
- contract: `Wire(groups, WireRules{Forbidden, Except})` audits the response types of the typed routes by reflection: an exported field without a json name, and an output type declared in a forbidden package.
- database/types: `ColumnTypes()`, the entity generator's column-type map (tinyint(1) as Bool, NOT NULL zero-date defaults as ZeroDate/ZeroDateTime, enum('0','1') as LegacyBoolEnum, Null* for nullable columns), for gorm.io/gen's `WithDataTypeMap`; go-core does not import gen.
- dbtest: `Tx(tb, dsnEnv)`, a rolled-back transaction on an existing MySQL/MariaDB schema: skips when the variable is unset, adds `clientFoundRows=true`, one connection, rollback then pool close on cleanup.

### Fixed

- dbtest: `RequirePortable` no longer reports "no .sql files" for an FS whose SQL is in subfolders (`local/*.sql`).
- `cmd/go-core` is part of the root module, so `go run github.com/wssto2/go-core/cmd/go-core@vX` resolves with every go-core tag (only the nested `cmd/go-core/v1.10.0` was ever tagged). go-core's go.mod gains cobra, huh and lipgloss; an application's go.sum is unchanged.

## v1.15.1 (2026-10-17)

### Fixed

- resource: `WithAuthorLoader` reads an author or editor column of type `types.Int` or `*types.Int` (it read 0, so `meta.author` and `meta.editor` stayed null); `int`, `*int` and `types.NullInt` are unchanged.

## v1.15.0 (2026-10-16)

The last generic pieces an application kept to itself: hooks that let an app run its own user administration on identity's routes and screens, transaction and MySQL error helpers, env loaders for mail, incident alerts and push, and a go2ts that keeps a generated folder in sync.

### Added

- identity: `WithUserReach`, a per-person reach check asked by every users route that names a person (show, update, password, deactivate, activate, unlock, sessions, revoke, sign-ins, changes, activity) with that route's permission. A person out of reach answers 404 `identity.account.not_found`, the same as a missing id; a guard test fails when a new person route forgets it. Which people the list shows stays the Searcher's to scope.
- identity: `WithUserExtension` (`account.UserExtension`): the app's own data as `ext` on list rows and the record; create, update and deactivate take `ext` / `details`, run in identity's transaction (an error rolls everything back) and answer `ext`. `Admin.CreateWithExt` and `Admin.DeactivateWithExt`.
- identity: `WithChangeLog` and `account.ChangeEntry.Ext` (as `ext` on a change row); `WithIdleAfter` and the list filter `last_sign_in=never|idle`; `account.Query` gains `Filters` (query keys identity has no field for reach the app's Searcher, in `Search` and `Counts`), `LastSignIn` and `IdleBefore`. identitytest: `WithUserExtension`, `WithIdleAfter`, `WithSearcher`, `WithChangeLogWrap`, `Accounts.WithSignIns`; storetest: `accounts/last sign-in filter`.
- database: `BaseRepository.InTx` and `WithinTx` (a nested call joins the caller's transaction) and `PlainRepository`, a repository whose connection ignores any transaction in the context. `IsDuplicateKey`, `IsDuplicateKeyNamed` and `IsDeadlock` for MySQL, MariaDB and SQLite; event, authz/gormstore and incident/gormstore use them instead of private copies.
- datatable: `ParamsOf` (pagination.Page to QueryParams) and `DatatableResult.ToPageResult`; resource: `Response.ToPagination`.
- web: `SignedInAs[T]`, the signed-in person as the application's own user type (401 when there is none or it is another type).
- mail: `SMTPFromEnv(defaultFromName)` and `SenderFromEnv(defaultFromName, logger)` read `SMTP_HOST`, `SMTP_PORT` (587), `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM_ADDRESS`, `SMTP_FROM_NAME` and `MAIL_REDIRECT_TO` (a bad address is an error naming the variable). incident: `AlertsFromEnv` reads `INCIDENT_NOTIFICATIONS_ENABLED`, `INCIDENT_IMMEDIATE_SEVERITIES`, `INCIDENT_IMMEDIATE_COOLDOWN_MINUTES` and `INCIDENT_DIGEST_INTERVAL_MINUTES`. notification/push: `ConfigFromEnv` reads `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY` and `VAPID_SUBJECT`, and `Config.Enabled`.
- go2ts: `Sync` writes a generated folder so only changed files are written and stale `.ts` files are deleted; a json tag with `omitzero` is optional, like `omitempty`.

### Changed

- identity: `SetPassword` by an administrator for themselves keeps their own session (`account.SetPassword.KeepToken`).
- identity: `POST /v1/iam/users/:id/deactivate` answers 200 `{}` (or `{ext}`) instead of 204.
- identity: `Admin.RevokeSession` of a missing account is `identity.account.not_found` (it was `session.not_found`, which told who exists). New reason `identity.list.last_sign_in_invalid`.
- go2ts: `json.RawMessage` is `unknown` / `z.unknown()` (it was an array of numbers).
- `go-sql-driver/mysql` is a direct dependency.

## v1.14.0 (2026-10-16)

A development copy of production data must not mail real people.

### Added

- `mail.Redirect(next, to) Sender`: every message goes to the one address `to`, whatever its recipients were, and is sent once. `Message` has no Cc, Bcc or headers, so the original recipients are put in the subject, `[to a@x.hr, b@y.hr] Subject`. Text, HTML and Inline are untouched and an error from `next` is returned as it is (`ErrPermanent` stays detectable). An address that is not an e-mail address is reported by every `Send`, like an `SMTP` configuration that cannot work; the message is never sent to its own recipients.

## v1.13.0 (2026-10-16)

Backend pieces an application kept to itself become library parts: named storage disks and a file response, inline images and a branded frame for mail, paper-form PDF widgets, a retention runner, an opt-in check for legacy utf8mb3 text and a safe `Content-Disposition`.

### Added

- `storage`: named disks. `app.Disks(storage.Disk(name).Local(root) | .Memory() | .Driver(d) [.Public(baseURL)])` declares them; `storage.Use(app, name)` takes one as `*storage.Files`, whose `Folder(prefix)` scopes it (nothing outside can be read, listed or deleted; `..`, absolute paths and backslashes are refused; folders nest). A disk is private unless it says `Public`; `URL` is an error on a private disk and `base/folder/key` (escaped) on a public one. An unknown or duplicate disk, a disk without a driver, or an unusable root stops start-up with the other problems.
- `storage.Opener` (`Open(ctx, key) (io.ReadSeekCloser, fs.FileInfo, error)`), implemented by `storage/local` and `storage/memory`.
- `web.Serve(c, disk, key, disposition)` streams a stored file with `http.ServeContent` (Range, Last-Modified, If-Modified-Since) when the driver can seek, plainly otherwise; a missing file is the JSON 404. `web.Attachment(name)` and `web.Inline(name)` build the disposition.
- `web.Disposition(kind, name)`: a safe `Content-Disposition` value for `inline` or `attachment`, built with `mime.FormatMediaType`. Quotes and backslashes are escaped; a name with non-ASCII characters gets an ASCII `filename=` fallback and an RFC 5987 `filename*`; a directory part and control characters (CR/LF included) are dropped; a name is cut at 200 bytes; an empty name gives the bare kind. The value is never empty or invalid.
- `mail.Message.Inline` and `mail.Content.Inline` (`mail.Inline{CID, ContentType, Name, Data}`): parts the HTML shows with `cid:`. With them an HTML mail is `multipart/alternative` (text, then `multipart/related` with the HTML and each part, `Content-ID` and `Content-Disposition: inline`, base64); without them the message is exactly as before. `Content.To` and `Sink` carry and record them. `Message.Check()` refuses (`ErrPermanent`) inline parts without HTML, a CID with anything but letters, digits and `. _ ~ + - @`, a repeated CID, an unparsable content type, a control character in the name and empty data.
- `mail.Brand`, `mail.Page`, `mail.Row` and `Brand.Render(Page) (Content, error)`: an application's HTML mail frame (a fixed 560 px table layout; the brand sets the logo, colour, language, greeting and footer) with a plain-text alternative made from the same page. The logo travels as `Content.Inline`. The zero `Brand` is a plain frame. Text and links are escaped; a `javascript:` link is not made a button.
- `pdf/widget`: paper-form widgets `SignatureLine(caption, value, spaceAbove)`, `LabelWithValue(label, value, width)` (`FillWidth` fills the row, 0 is the text width), `TextLines(n)` (ruled writing lines), `CodeBoxes(n)` (comb boxes), `YesNo(yes, no, yesLabel, noLabel)` (two labelled checkboxes; the captions are the caller's) and `PageBreak()`. The defaults are a compact form look (8pt text, 0.2 mm black rules, 7pt Yes/No captions); `FieldFontSize` and `FieldRule` restyle them.
- `retention`: housekeeping for the tables an application owns. `retention.New(db, clock, rules...)` returns a `*retention.Pruner` and checks every rule at construction (plain identifiers for the table, the `OrderBy` and the `Set` columns; a `Where` that uses `@cutoff`; a positive `Keep`; unique names; the errors wrap `ErrInvalidRule`). `Run(ctx, Options{DryRun, Only})` takes one reading of the clock and deletes (or, with `Set`, redacts) the rows past `Keep` in batches of 500 until one comes back short, and returns per rule `Cutoff`, `Matched` and `Affected`. A failing rule does not stop the others. It uses only single-table `DELETE/UPDATE … ORDER BY … LIMIT`, so it runs on MySQL 8 and later and MariaDB 10.3. go-core ships no rule; the application owns them.
- `validation.CheckStorable(in)`: an opt-in check for an input whose strings land in utf8mb3 columns. It walks every `string` and `*string` field it can reach (nested structs, pointers, slices and arrays, by json path, skipping `json:"-"` and unexported fields; maps are not walked) and answers a 422 `*ValidationError` with `validation.CodeUnsupportedCharacters` (`unsupported_characters`) on each field holding a 4-byte UTF-8 character such as an emoji. It is not part of `ValidateInput`: go-core's databases default to utf8mb4, so only an application on legacy utf8mb3 tables calls it, from its input's `Validate()`. The application translates the code. `validation.HasUnsupportedChars(s)` is the per-string predicate.

### Changed

- `storage/local.Put` writes through a temporary file renamed into place: a failed write leaves no partial file and keeps the old one. A declared `size` is the exact length; a longer or shorter stream is a 400 instead of a silently cut file; 0 means unknown. The same for `storage/memory`, where `size=0` used to store an empty file.
- `Delete` of a missing key is not an error in `storage/local` (it was a 404). `List("")` lists the whole root, keys use forward slashes, and `storage/memory.List` matches by directory.

## v1.12.0 (2026-10-15)

The saved views of a list become a module. A person keeps the filters and search of a list under a name and applies them again with one click.

### Added

- `savedview`: `savedview.Install(app)` (no options, no permission) and `savedview.Routes` (group `savedview`): `GET /v1/saved-views?list=` (`savedview.index`), `POST /v1/saved-views` (`savedview.store`) and `DELETE /v1/saved-views/:id` (`savedview.delete`), all person-only. A view is `{id, list_key, name, state, updated_at}`, `state` an object.
- SAVEDVIEW-001: a view is the signed-in person's alone (the id comes from the session; another person's view is a 404 on delete); saving under an existing name of the list replaces the state; at most 20 views per person and list (a new name beyond that is a 400, replacing one is allowed); `state` is a JSON object of at most 4000 characters, `list_key` at most 100, `name` at most 60.
- Table `saved_views` (`savedview/migrations`; the file is an application's `saved_filters` table after renaming the table and its two indexes, so it adopts it with `MarkApplied`), the GORM store `savedview/gormstore` and its conformance suite `savedview/storetest`.
- `cmd/modulets` writes `savedview/`.

## v1.11.1

### Fixed

- The static and Vite asset routes answer 404 for `*.map`: a build's source maps carry the original source, which a release may keep for reading a minified stack but never serves to a browser.

## v1.11.0

The incident log becomes a module. A server error or a panic is an incident, repeats are counted on it, a person can report a bug, the browser can report its errors, and the people who hold `incident:manage` are alerted.

### Added

- `authz.Engine.HoldersOf(ctx, permission) ([]Subject, error)`: every subject bound to a role (predefined, computed or custom) that grants the permission, each once, in the order of their first binding, with their kind (a service account is returned as one; callers filter). It needs a store that lists custom roles and their holders (`authz/gormstore` and `authztest.MemoryStore` do); the `Store` interfaces are unchanged.
- `incident`: `incident.Install(app, users, opts...)`, `incident.DefinePermissions(catalogue)` (`incident:{view,create,update,manage}`, only the ones the catalogue lacks), `incident.Routes` (group `incident`, under `/v1/incidents`), the options `incident.Capture{TrailSize, Environment, Module}`, `incident.Alerts{Enabled, Notices, Immediate, Cooldown, DigestEvery, Holders}`, `incident.ClientErrors(limiter)`, `incident.Retain(d)` and `incident.Link(fn)`, `incident.AlertCategory` (the notification category of the alerts) and `incident.Opened` (the event of a new incident). `Install` returns `*incident.Incidents` with `Service`, `Sweep` and `FlushDigest`.
- Capture: engine middleware (`app.Use`) that records a response of 500 or more and a panic (recovered, recorded, raised again: the 500 answer does not change), each in a transaction of its own, with the request id, route pattern, module, caller (a person, or a service account recorded as one and not as user 0), tenant, client address, masked query, headers with credentials redacted and the caller's last 30 calls under `app.Prefix()` (INC-TRAIL-001). Repeats are grouped by a fingerprint under one open incident (INC-FINGERPRINT-001, INC-CLASSIFY-001).
- `POST /v1/incidents/client-errors` (204): a signed-in person reports an error the browser caught: source `client_error`, severity low, staff only, grouped by message and top stack frame, every field bounded, a per-person rate limit (the excess is dropped with the same 204).
- Alerts (off by default): a new incident publishes `incident.opened` in its transaction; a consumer sends an immediate severity (default critical and high) at once to the people who hold `incident:manage`, as a high priority notification, within a per-severity cooldown (default 5 minutes, **started only after a successful send**); the rest waits for a digest sent every 30 minutes. The people are those `authz.Engine.HoldersOf("incident:manage")` returns (users only), or from `Alerts.Holders`. Wording in `incident/mailtext` (en, hr, bs, sl).
- Housekeeping: occurrences older than 90 days are deleted except the newest of each incident; digest items are deleted a day after they went out.
- The same six routes the first application served (list, show, comments, report 201, comment 201, update) with the same paths, status codes and permission ids; the people on an incident are `{id, name, email}`.
- Tables `incidents`, `incident_occurrences`, `incident_reports`, `incident_comments`, `incident_watchers`, `incident_alert_cooldowns` and `incident_digest_queue` (`incident/migrations`; the first seven files are an application's `bug_*` and `incident_notification_*` tables after a rename, so it adopts them with `MarkApplied`), the GORM store `incident/gormstore` and its conformance suite `incident/storetest`.
- `cmd/modulets` writes `incident/`.

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
- `pagination`: the framework-free paging primitives `Page`, `PageResult`, `ViewCount` and `Resource` (with `MapResult` and `MapResource`), for a domain or application layer that must not import `datatable`.
- `web` helpers for typed routes, moved from an application's `httpx`:
  - `Mapped(from)(value, err)` and `Value(record)` shape a service's answer into a route's output;
  - `PositiveID(name, id)` answers 400 and `PositivePathInt(key, id)` answers PathInt's 422, for an id the typed route has already read (`PathInt` and `GetPathIDNamed` now build their answers from the same code);
  - `SignedIn(ctx)` is the signed-in person or service account, or 401;
  - `PageFromValues(url.Values)` builds a `pagination.Page` from the paging parameters `datatable.ParamsFromValues` reads;
  - `WritePage`, `WriteResource` and `WriteAccepted` write a `pagination.PageResult`, a `pagination.Resource` and a 202.
- `web/typed`, the helpers that need `route`: `ToPage` and `Record` (with `RecordMeta`) are the typed outputs that answer what `WritePage` and `WriteResource` write, and `AccessToken(ctx)` is the session's token from the cookie or the bearer header.
- `web/upload.ValidateHeader(header, maxBytes, allowedMIME)`: size and content-sniffed type of a multipart header a typed route already holds.
- `web/webtest`: `AssertGolden`, `AssertSameWire` and `Capture`, the golden-byte assertions for a DTO's wire shape.
- `cmd/go-core new feature --domain d --feature f [--entity E]`: the layered skeleton of a feature in the module of the current directory (domain entity and repository port, application service, inputs and permissions, an in-memory repository, typed routes with handlers and DTO, and `Install`). It reads the module path from `go.mod`, never overwrites a file, and prints the steps left to do by hand.

### Changed

- **Breaking:** `App.Check` (and so `Run`) refuses a route, typed, `Raw`, `Root` or `RoutesWith`, that declares none of `Requires`, `RequiresAny`, `RequiresAll`, `Public`, `PersonOnly` or `Authenticated`. The problem names the method, the served path and the choices. Add `.Authenticated()` to a route that was meant for any signed-in caller: behaviour is unchanged.
- `access.bindable` (`GET /v1/iam/bindable-roles`) and `access.me` (`GET /v1/me/access`) declare `.Authenticated()`.
- `github.com/gabriel-vasile/mimetype` is a direct requirement (it was already in the module graph, through gin).

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
