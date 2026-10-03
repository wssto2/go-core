# go-core

## Overview

go-core is a modular Go backend framework that provides a shared foundation for building production-grade services.

It standardizes:

* Authentication & authorization
* Database access & transactions
* Validation & binding
* Error handling
* Middleware & HTTP layer
* Audit logging
* Multi-tenancy
* Event system
* Worker processes

The goal is to eliminate boilerplate and enforce **safe, predictable patterns** across all services.

---

## Key Principles

* Strong typing over dynamic behavior
* Explicit architecture (no hidden magic)
* Thin handlers, fat services
* Centralized error handling via `apperr`
* Context-driven request lifecycle
* Modular design (plug-and-play packages)

---

## Project Structure

### Core Packages

* `gocore` → assembles an application from features: `New`, `Install`-style features, `Later`, `Run` (see "Installing features")
* `route` → typed routes: `route.Get[In, Out](path).Requires(permission)`, binding, validation, permission check
* `gocoretest` → a `gocore.App` for tests: in-memory SQLite, fixed clock, `Do` / `Decode` to call routes
* `bootstrap` → application lifecycle, DI container, config (what `gocore` is built on)
* `web` → HTTP handling, response format, helpers
* `auth` → authentication, JWT, policies, middleware
* `authz` → roles, bindings at a scope, per-record access, delegation, list filtering (see `authz/doc.go`; sub-packages `authzgorm`, `gormstore`, `authzhttp`, `authzts`, `authztest`, `storetest`)
* `database` → repositories, transactions, types
* `validation` → input validation system
* `binders` → request binding (JSON, multipart)
* `middlewares` → HTTP middleware (auth, logging, security)
* `worker` → background workers

### Feature Packages

* `access` → roles and access administration over `authz`: role editor, bindings with delegation, effective access, routes and TypeScript contract (see "Roles and access"; core `access/admin`, routes `access/accesshttp`)
* `audit` → audit logging and diff tracking
* `datatable` → filtering, pagination, query helpers
* `tenancy` → multi-tenant context + DB scoping
* `event` → typed events and the per-consumer queue behind them (see "Events"; tables in `event/migrations`)
* `identity` → accounts, password sign-in, sessions, the lock after wrong passwords, the `/auth/me` payload, user administration and each person's profile with e-mail codes (see "Sign-in" and "Users and profile"; core `identity/account`, routes `identity/http`, store `identity/gormstore`, mail text `identity/mailtext`, tests `identity/identitytest`)
* `mail` → the `Sender` port, SMTP over the standard library, a recording `Sink` for tests, per-locale `Renderer` (see "mail")
* `notification` → the in-app inbox: categories, `Send` from an event consumer, the live stream, the inbox and dead-letter routes (see "Notifications"; migration in `notification/migrations`)
* `navigation` → the menu tree an application declares and the filter by held permissions

### Utility Packages

* `apperr` → structured application errors
* `i18n` → localization helpers
* `go2ts` → Go → TypeScript type generation
* `utils` → generic helpers

### Example App

* `go-core-example` → reference implementation

---

## How It Works

### Request Flow

1. HTTP request enters via `engine/gin.go`
2. Middleware chain executes (`middlewares`)
3. Request is bound using `binders`
4. Input validated via `validation`
5. Handler calls service layer
6. Service uses repositories (`database`)
7. Errors returned as `apperr.AppError`
8. Response formatted via `web/response`

---

## Installing features

A feature is a package with one `Install` function. Collaborators are ordinary arguments; app-wide values (database, clock, config, logger) come from the `*gocore.App`. The snippets below are taken from the compiled `Example` functions in `route`, `gocore` and `gocoretest`.

**Declare routes as values** (`route/example_test.go`). The declaration can be read without running anything; the handler is a plain function the compiler ties to it. Input fields say where they come from: `path:"id"`, `query:"page"`, or `json:"title"` for the body.

```go
var (
    Show   = route.Get[ShowInput, Ticket]("/tickets/:id").Name("tickets.show").Requires(PermView)
    Close  = route.Delete[ShowInput, route.Empty]("/tickets/:id") // 204
    Routes = route.Group("tickets", Show, Close)
)

func showTicket(ctx context.Context, in ShowInput) (Ticket, error) { ... }
```

What the typed form cannot express (Server-Sent Events, downloads, uploads) is declared with `route.Raw(method, path)` (same `.Name`, `.Requires`) and bound the same way with `.To(ginHandler)`; it is part of the route table without input or output types.

**Install the feature** (`gocore/example_test.go`):

```go
func Install(app *gocore.App) {
    service := &Service{Clock: app.Clock()}

    _ = app.Database()          // the primary connection; app.Database(Shared) another one

    app.Routes(Ping.To(service.Ping))
}
```

**Secure by default.** Every route needs an authenticated principal unless it is declared `.Public()`; `.Requires(perm)` adds a permission check on top. The application sets authentication once, with the middleware go-core already has, and non-public routes are mounted behind it:

```go
app := gocore.New(cfg,
    gocore.WithAuthentication(auth.Authenticated(provider), authzhttp.Principals(resolve)),
    gocore.WithAuthorizer(engine))

var Login = route.Get[route.None, Form]("/login").Public()   // anyone, signed in or not
```

An unauthenticated request to a non-public route gets the usual 401 envelope. A non-public route with no authentication configured is a start-up problem ("pass gocore.WithAuthentication(...) to gocore.New, or mark the route .Public()"). `route.Spec.Public` is part of the declaration, so generators can see it.

`const Shared database.Connection = "shared"` names a connection once; `Registry.Database(Shared)` and `app.Database(Shared)` take it, and `Registry.Get("shared")` keeps working.

**`main` reads top to bottom.** A feature used before the line that creates it does not compile (`gocore/testdata`, built by `TestInstallOrderIsCheckedByTheCompiler`):

```go
func main() {
    app := gocore.New(config, gocore.WithPrefix("/api"))

    users := identity.Install(app, identity.WithMail(sender), identity.WithCodeSecret(secret),
        identity.AllowImpersonation("iam.user:impersonate")) // sign-in, users, profile, authentication
    access.Install(app, permissions.All, users)               // roles and bindings; builds the engine
    tickets.Install(app, users)

    if err := app.Run(); err != nil { log.Fatal(err) }
}
```

identity needs no engine: `/auth/me` and `AllowImpersonation` read the application's authorizer (the engine `access.Install` builds) when a request comes, and `*identity.Users` is the `access.SubjectDirectory`. `internal/e2e` installs exactly this and signs in on SQLite, MySQL and MariaDB.

`Install` only collects (`Routes`, `Background`, `Permissions`, `Modules`); nothing runs until `Run`, so the order of those never matters. Old-style `bootstrap.Module`s run in the same app through `app.Modules(...)`, so an application moves to `Install` one feature at a time.

**A real cycle between two features uses `gocore.Later`:**

```go
reads := gocore.Later[contracts.VehicleReads](app)
vehicles := vehicles.Install(app, reads)
crm := crm.Install(app, vehicles)
reads.Set(crm.VehicleReads) // inside vehicles: reads.Get()
```

**`Run` checks first and says what to fix.** Every problem is collected, each with its fix (`ExampleApp_Check`):

```
gocore: cannot start, 2 problem(s):
  1. database: connection "shared" not found ... (registered connections: local). Fix: register the connection in the database config, or fix its name
  2. gocore.Later[gocore_test.Reads] was never set. Fix: call Set on it once every feature that needs it is installed
```

The check covers an unset `Later`, a declared route with no handler (typed or raw), a `Requires` permission the catalogue does not define, a non-public route with no authentication or a permission route with no authorizer, duplicate or refused routes, and an unknown connection. Then it boots; a failed boot stops what already started, in reverse. Shutdown order: HTTP, background workers and modules, database last.

**Test a feature with plain calls** (`gocoretest/example_test.go`):

```go
app := gocoretest.New(t, gocoretest.SignedIn(authz.User(7, 0))) // in-memory SQLite, fixed clock, test logger; requests are signed in as user 7 (without SignedIn they are anonymous: 401 except Public routes)
Install(app)

rec := gocoretest.Do(t, app, http.MethodGet, "/tickets/7", nil)
ticket := gocoretest.Decode[Ticket](t, rec)
```

### API prefix and versions

`gocore.New(cfg, gocore.WithPrefix("/api"))` mounts every route under one app-wide prefix (`app.Prefix()` returns it, for cookie paths and the like). The prefix is only where the API lives; it carries no version. Routes declare their version themselves: a module's route is `route.Get[...]("/v1/iam/roles")`, served at `/api/v1/iam/roles`. Modules never take a prefix. A future v2 is new declarations next to v1 in the same module (`ListRolesV2 = route.Get[...]("/v2/iam/roles")`, `RoutesV2 = route.Group("accessv2", ...)`): both are served, and the TypeScript is generated into separate folders. `gocoretest` apps use the prefix too.

### Migrations

An application keeps its own goose files in one directory per connection; a module ships a flat `embed.FS` and hands it over with its connection. Both are collected by `Install`; none runs until you say so (`gocore/example_test.go`):

```go
app.MigrationsByConnection(migrations)            // migrations/local/..., migrations/shared/...
app.Migrations(authzmigrations.Files)             // a module's files, on the primary connection
app.Migrations(authzmigrations.Files, Shared)     // or on another one
```

`Run` never migrates. It reads the command line, so the deploy is `./myapp migrate`, then `./myapp`:

```
./myapp                  check, then serve
./myapp migrate          apply every pending migration (app and modules, every connection), then exit
./myapp migrate status   list applied and pending migrations per connection, then exit
./myapp help             list the commands (an unknown one exits non-zero)
```

Commands never serve or boot modules; they use the database connections and close them. While any migration is pending, starting to serve stops with one start-up problem whose fix is `run "myapp migrate" before starting it`. `app.Migrate(ctx)` is public for tests and for applications with their own command line; `gocoretest.New` migrates as features are installed. `RunCommand(ctx, args, out)` takes the arguments, for tests.

All sources of a connection run as one set in version order; two files with one version stop the run and name both. A module's files use its release date as version and never change once tagged.

**Test on every database** (`database/dbtest`). `dbtest.Run(t, func(t *testing.T, db *gorm.DB) { ... })` runs on SQLite always, and on MySQL and MariaDB when `GOCORE_MYSQL_DSN` / `GOCORE_MARIADB_DSN` are set (`root:pw@tcp(127.0.0.1:3310)/`). A server run gets an empty schema it creates and drops itself, so it may run DDL and use several connections; the others skip with a message. `dbtest.RequirePortable(t, migrations.Files)` fails on what MariaDB 10.3 rejects (SKIP LOCKED, FOR UPDATE OF, FOR SHARE, JSON_TABLE, `->>`, RETURNING, window functions). `authztest.MariaDB` uses the same schemas; `AUTHZ_MARIADB_DSN` still works.

### TypeScript contract

A feature's `Routes` group (`route.Group("tickets", ...)`) is its contract. One line per feature generates everything, without building the app or opening a database (`contract/example_test.go`):

```go
contract.Generate("frontend/generated", tickets.Routes, leads.Routes)
```

writes `frontend/generated/tickets/{entities,schemas,routes}.ts`: the output types, the input types as Zod schemas (keeping `max:` bounds), and the route table the client builds typed requests from:

```ts
import { route } from "@wssto2/vue-core/client";
export const ticketsRoutes = {
  show: route<ShowInput, Ticket>("GET", "/tickets/:id", { permission: "tickets.ticket:view" }),
  events: route.raw("GET", "/events", { public: true }),
} as const;
```

A route whose output is `datatable.DatatableResult[Row]` types as `ListResult<Row>` (imported from `@wssto2/vue-core/client` next to `route`, with exactly the datatable wire shape); any other generic type is refused. A body field with `json:",omitempty"` is `.optional()` in the Zod schema, unless it is `required`. A typed route's path and its input must agree: every `:name` of the path needs a field tagged `path:"name"` and the other way round, which start-up (`Check`) and `contract.Generate` report with the route, the parameter and the fix, since TypeScript cannot check it.

The key is the route's `.Name` without the group prefix (`tickets.show` is `show`); without a name it is the method and path (`GET /tickets/:id` is `getTicketsById`). `route.None` and `route.Empty` become `void`. Every file's first line names the go-core version that wrote it.

#### Enums

A named string type with a fixed set of values is declared once, listed in the group, and every field of that type becomes the union in `entities.ts` and a `z.enum` in `schemas.ts` (`route/example_test.go`, `ExampleEnum`):

```go
type Status string

const (
    StatusActive Status = "active"
    StatusLocked Status = "locked"
)

var Statuses = route.Enum(StatusActive, StatusLocked) // the type is inferred from the constants

var Routes = route.Group("identity", List, Show).Types(Statuses)
```

```ts
export type Status = "active" | "locked";
export type UserRow = { id: number; status: Status; /* … */ };
// schemas.ts: export const StatusSchema = z.enum(["active", "locked"]);
```

A named string type that is not declared stays `string`: nothing is guessed. The TypeScript name is the Go type's; `route.Enum(...).As("SubjectKind")` gives another (for a type from another package, or a name that clashes with a DOM global such as `Event`). A field named `ID` is `number`; only a pointer (`*int`) is `number | null`.

### Roles and access

`access` is the administration of what `authz` decides: which roles exist, who holds them where, and who may give them. One line puts it into an application (`access/example_test.go`):

```go
acc := access.Install(app, catalogue, users) // users names the people who can hold roles
```

`catalogue` is the application's `*authz.Catalogue` (define the application's permissions first: building the engine freezes it); the module adds its own five permissions when the catalogue lacks them (`iam.role:{view,manage,delete}`, `iam.user:{view,manage}`). `users` is an `access.SubjectDirectory`: `SubjectNames(ctx, subjects)` returns the display name of each person or service account that exists. Install builds the engine, mounts the routes, registers the three authz tables' migrations and makes the engine the application's authorizer; a missing piece is reported by `Run` with its fix.

Options, all named: `access.WithRoles(...)` the roles that live in code (computed ones too), `access.WithScopes(hierarchy, catalogue)` places below the root (without it `organization` is the only one), `WithFeatures`, `WithAudit(repo)` (an audit row for every role and binding change, in the same transaction), `On(connection)`. The five permission ids are fixed, so the generated TypeScript always matches.

The first administrator is made outside the delegation rules, from a seed or a command, because nobody holds the permission to give roles yet:

```go
err := acc.Seed(ctx, authz.Subject{Kind: authz.KindUser, ID: 1}, "webmaster")
```

Everything after that goes through `authz.Admin`'s delegation: a role may be given only where the giver holds the binding permission, never one that holds a System permission the giver lacks or an organization-only one unless they manage bindings at the root, never to yourself (`authz.self_assignment`), and nobody removes their own last binding permission (`authz.last_admin`). A custom role may not grant more than its author holds (`authz.escalation`). The rules, with their Logic IDs, are in `docs/rules/access/authorization.md`.

The routes are declared under `/v1` (an app with `gocore.WithPrefix("/api")` serves `/api/v1/iam/roles`): `GET|POST /v1/iam/roles`, `GET|PUT|DELETE /v1/iam/roles/:ref`, `/:ref/holders`, `/:ref/compare?with=`, `POST /:ref/replace`, `GET /v1/iam/bindable-roles?level=&scope_id=`, `GET /v1/iam/users/:id/{access,scopes}`, `POST /v1/iam/users/:id/bindings`, `DELETE /v1/iam/users/:id/bindings/:binding_id` and `GET /v1/me/access`. `access.Routes` is their declared contract (`contract.Generate("frontend/generated", access.Routes)`); lists are plain `{roles: [...]}` objects, not pages.

Without the HTTP layer the services are plain methods: `acc.Roles.Create(ctx, admin.RoleDraft{...})`, `acc.Bindings.Bind(ctx, subject, admin.BindingDraft{...})` (package `access/admin`, which imports only `authz` and `apperr`).

---

## Error Handling

All errors use `apperr.AppError`:

* Structured error codes
* Log level hints
* Field-level validation errors

Example:

```go
return apperr.BadRequest("invalid input")
```

---

## Authentication

* JWT-based authentication
* Policy-based authorization (`namespace:action`)
* Context-based user injection

```go
user := auth.MustGetUser[MyUser](ctx)
```

### Sign-in

`identity` is the sign-in module: accounts, password sign-in, sessions, the lock after wrong passwords, signing in as somebody else, and the `/auth/me` payload the client starts from. One line puts it into an application (`identity/example_test.go`); it also makes it the application's authentication, so every route that is not `.Public()` is behind it and the signed-in person is the authz principal:

```go
users := identity.Install(app, identity.WithMail(sender), identity.WithCodeSecret(secret)) // routes, tables, authentication
```

Mail is the one thing identity cannot default (see "Users and profile" below): pass `WithMail(sender)` and `WithCodeSecret(secret)`, or `identity.WithoutMail()`; without either, start-up stops and says which. It uses the GORM store, bcrypt and its own migrations unless told otherwise. Options are named for what they change: `identity.On(Shared)` (tables on another connection), `WithAccounts(store)` (your own accounts table), `WithHasher(h)`, `WithRefreshHasher(h)`, `WithConfig(identity.Config{Lock: identity.Lock{After: 3}})`, `WithAccess(a)` (override where the payload's `authz.MyAccess` comes from; by default the application's authorizer, once `access.Install` is part of it), `WithNavigation(menu...)` (and the menu, cut to the permissions held), `WithUserProjector(fn)`, `AllowImpersonation("iam.user:impersonate")` (checked through the application's authorizer), `WithNotices(n)`, `WithCookies(...)`. `users` is what other features take: `users.Get(ctx, id)`, `users.ChangeLocale(...)`, `users.Sessions(ctx, id)`, `users.RevokeSessions(...)`.

The routes are declared under `/v1/auth` (an app with `gocore.WithPrefix("/api")` serves `/api/v1/auth/login`): `POST login` and `refresh` (public), `POST logout`, `GET me`, `POST change-locale`, `POST login-as` and `POST login-as/return` (back to one's own account with no password; any other session is refused with `identity.impersonation.not_active`). `identity.Routes` is their declared contract. Tokens travel in HttpOnly cookies (`access_token`, and `refresh_token` for the refresh route only) and an access token is also accepted as `Authorization: Bearer`. Login, refresh and `me` answer the session payload, which vue-core reads with `parseSessionPayload`:

```json
{"success": true, "data": {
  "user": {"id": 1, "login": "ana", "name": "Ana Anić", "email": "ana@example.test", "locale": "hr"},
  "expires_at": "2026-01-03T03:04:05Z",
  "access": {"subject": {"kind": "user", "id": 1}, "root": false, "permissions": {}},
  "navigation": [{"i18n": "nav.tickets", "route": "tickets.index", "permissions": ["tickets.ticket:view"]}]
}}
```

When the session was opened by signing in as somebody (`login-as`), the payload also has `"impersonator": {"id": 1, "name": "Ana Anić"}`, the person really signed in; for a person's own session the key is absent. `user` is what the projector makes of the account (the default has `id`, `login`, `name`, `email`, `locale`, never the password hash), `access` is the authz engine's answer and `navigation` your menu filtered by it. Refusals are `apperr` reasons with params, never sentences: `identity.signin.failed` (an unknown login and a wrong password answer alike, 422), `identity.signin.locked` (`params.locked_until`), `identity.signin.inactive`, `identity.session.invalid`. Five wrong passwords lock sign-in for fifteen minutes, ten attempts a minute per login are let through; the rules, with their Logic IDs (IAM-USER-001 to 004 and 008), are in `docs/rules/identity/signin.md`.

#### Users and profile

The same `Install` serves user administration under `/v1/iam/users` and every signed-in person's own profile under `/v1/iam/profile`; both are in `identity.Routes`. Administrators need `iam.user:view` (read) and `iam.user:manage` (write), the ids `access` uses for the same two ideas, and `iam.user.activity:view` (a `System` permission) for a person's activity (`access.Install` defines them in your catalogue; an application without access calls `identity.DefinePermissions(catalogue)`). A profile needs a signed-in person and nothing else.

```go
// sender is a mail.Sender: mail.SMTP(...) in production, mail.NewSink() in a test or a first run.
users := identity.Install(app,
    identity.WithMail(sender),
    identity.WithCodeSecret(os.Getenv("CODE_SECRET")), // 32 characters or more; keys the stored one-time codes
    identity.WithDeactivationHook(handOver),           // the application takes part in a deactivation, or refuses it
)

// The first administrator is made in code:
_, err := users.Admin().Create(ctx, account.CreateAccount{
    Login: "admin", Name: "Admin", Email: "admin@example.com", Locale: "en", Password: "a long password",
})
```

Administrators create, update, deactivate and activate people, give them a new password (which lifts their lock and ends their sessions), unlock them, read their sign-in history (`?view=all|failed`, the refused attempts) and the changes made to them (`?view=all|access|details`; access is a new password, a deactivation or an activation), each with its counts in `meta.views` and every row naming who acted as `{id, name}` (null: nobody; an empty name: the account is gone), from one batched lookup per page (go-core's `audit` trail, whose table `identity.Install` registers), and list or end their sessions. What a person *did* is `GET /v1/iam/users/:id/activity?area=&from=&to=` (the audit rows whose actor they are, newest first, each marked with who was signed in as them, with a count per area in `meta.views`): name the areas your records fall in with `identity.WithActivityAreas(identity.Area("crm").Types("customers").Prefix("contracts."))`: the keys are your i18n keys, a record type no area covers is `other`, the changes to accounts are `identity`. `GET /v1/iam/users` answers go-core's datatable page, which the generated TypeScript types as `ListResult<UserRow>` from `@wssto2/vue-core/client`: views `active` (the default), `locked`, `inactive`, `all`, counted under the same `search` in `meta.views` (every list with tabs sends `[{key, count}]`, `datatable.ViewCount`); `search`, `order_col`, `order_dir`, `page`, `per_page`.

A person changes their name and phone, their password (the current one is asked again; five wrong ones lock for fifteen minutes; every other session ends) and their e-mail address: a six-digit code is mailed to the *new* address and the change happens when it is typed in. One account per address (`identity.email.taken`). The code, its limits (60 seconds between sends, five an hour, five attempts, fifteen minutes) and the lock are in `docs/rules/identity/` (IAM-OTP-001 to 004, IAM-REAUTH-001, IAM-PROFILE-001 to 004, IAM-USER-005 to 007).

The text of identity's mails is English (`identity/mailtext`); give `identity.WithMailContent(renderer)` your own in the person's language, for the mails you want to change. `deactivation hooks` run in the transaction of the deactivation before anything is written, and what they write with the context they get commits or rolls back with it:

```go
handOver := identity.DeactivationHookFunc(func(ctx context.Context, a account.Account, actor int) error {
    if open, _ := leads.OpenFor(ctx, a.ID); open > 0 {
        return apperr.BadRequest("owns open leads").WithReason("crm.owns_leads", map[string]any{"count": open})
    }
    return nil
})
```

`identitytest.New` builds all of it over memory stores for a test (`kit.Admin`, `kit.Profile`, `kit.Mailbox` holding the code a person would have been mailed). `go run github.com/wssto2/go-core/cmd/modulets <dir>` writes the TypeScript of every go-core module (identity, access) for vue-core to commit.

#### Development server

`go run github.com/wssto2/go-core/cmd/devserver` serves identity and access for developing a front end (the vue-core playground signs in against it): `/api/v1/auth/…` and `/api/v1/iam/…` on `127.0.0.1:8090`, an in-memory SQLite database that starts empty on every run, e-mail codes printed to the log (`mail.NewSink`), and CORS with cookies for `http://localhost:5173`. Two people are seeded, the second with two records in her activity (`crm`, one done while the administrator was signed in as her), a refused sign-in on her history and a change the administrator made to her:

| login | password | role |
|-------|----------|------|
| `admin` | `admin-password` | `webmaster` (everything, so it may sign in as `user`: `POST /api/v1/auth/login-as {"user_id": 2}`, then `login-as/return`, to see the impersonation banner) |
| `user` | `user-password` | `seller` (`crm.customer:view`) |

Flags: `-addr host:port`, `-origin http://localhost:5173` (commas for several), `-allow-remote`. The passwords are public, so it refuses to start with `GO_ENV=production` or `APP_ENV=production`, or on an address other than this machine (`127.0.0.1`, `::1`, `localhost`) unless `-allow-remote` says so. Development only.

#### mail

`mail` is the sender port with an SMTP implementation over the standard library (STARTTLS or implicit TLS, `multipart/alternative` text and HTML, one deadline for the whole conversation), a `Sink` that records messages, and a `Renderer` (`mail.Templates(fsys)` over `<locale>/<name>.subject.txt|.txt|.html` files, falling back from `pt-BR` to `pt` to `en`; `mail.Fallback(mine, defaults)` to write only some mails).

```go
sender := mail.SMTP(mail.SMTPConfig{Addr: "smtp.example.com:587", Username: u, Password: p, From: "App <no-reply@example.com>"})
```

In a test, `identity.Install` runs on `gocoretest.New(t)`: SQLite tables are created from the models (`app.Schema`), the real SQL files are tested on MySQL and MariaDB. Feature tests that need people use `identitytest.Users(t, identitytest.Account(1, "ana", "secret"))`, which is the same service over memory stores.

---

## Database

* GORM-based repositories
* Transaction support via context
* Custom nullable & typed fields
* SQL migrations per connection (`database/migrate`, goose): `migrations/<connection>/<yyyymmddhhmmss>_<name>.sql`, each database records its own versions; `MarkApplied` adopts a database whose changes ran by hand. `go-core new migration <name> -c <connection>` creates one

---

## Events

A fact one feature reports and others react to is an `event.Event`, declared once as a value. The name is stored with every queued row, so it never changes with the Go type.

```go
var Assigned = event.Define[TicketAssigned]("tickets.assigned") // version 1; .Version(2) only when the payload's shape changes

func Install(app *gocore.App, users identity.Users) {
	notices := NewNotices(users)
	app.Events(Assigned.To("notifications.assignee", notices.Assigned)) // the consumer's durable name is the first argument
	// Assigned.To(...).Retry(event.Attempts(10), event.Backoff(time.Second, time.Hour)) // optional; the defaults are 5 attempts, 5 s doubling to 30 min
}

// in the write that the event reports
err := transactor.WithinTransaction(ctx, func(ctx context.Context) error {
	if err := tickets.Assign(ctx, id, user); err != nil {
		return err
	}

	return Assigned.Publish(ctx, TicketAssigned{TicketID: id, UserID: user}) // joins the transaction in ctx
})

// in a test
gocoretest.Publish(t, app, Assigned, TicketAssigned{TicketID: 7, UserID: 3}) // the consumers have run when it returns
```

* `Publish` joins the transaction `database.Transactor.WithinTransaction` put in ctx, so the event is queued if and only if the write commits. Without one it fails with an error saying so.
* `app.Events(...)` collects consumers and, with them, the queue's tables (`event/migrations`: `outbox_events` and `event_consumer_attempts`) on the primary connection. A feature that only publishes calls `app.Events()` with none. `Run` starts one worker per consumer and stops them with the other background work; `Check` refuses two consumers with one name, a name that is not lower-case words joined by dots or dashes, and two `Define` calls that share a name but not a payload.
* Each consumer has its own state per event: it claims the due events of its name, leases them for five minutes, retries a failing handler with a doubling backoff, and after its attempts sets the event aside as a dead letter. An event whose envelope, payload or version cannot be read is a dead letter at once. One failing consumer holds back no other, and the outbox row is marked processed once every consumer of it finished.
* **Delivery is at least once.** A handler whose lease ran out while it was still working can run beside a second delivery of the same event, and a dead letter put back runs again: a handler must tolerate being called twice. `event.ID(ctx)` is the event's id, the key to dedupe on.
* `event.NewDeadLetters(db, clock)` lists the dead letters and puts them back (`Retry` one, `RetryAll` of a consumer); its HTTP routes come with the notification module (see "Notifications").
* A housekeeper, added to the background workers by `app.Events`, deletes events every consumer finished more than 30 days ago (`event.Retention`) with their attempts, in batches by primary key. Dead letters are never deleted by it: their events stay for a retry. There is nothing to configure.
* The claim is MariaDB 10.3 safe (no `SKIP LOCKED`, no `FOR UPDATE OF`: candidates are read, their outbox rows locked by primary key, the consumer's attempts read and the leases written). The queue is tested on SQLite, MySQL and MariaDB 10.3, including three workers on one database never handling an event twice.
* Rows an application wrote with `event.InsertOutboxEvent` are named by the Go type (`"lead.AssignedEvent"`: package name and type name) with version 1; `event.Define[AssignedEvent]("lead.AssignedEvent")` consumes them unchanged. `outbox_events` has the DDL such an application already has.

---

## Notifications

In-app notifications: what a person finds when they open the application, read on one device and read on all of them. A feature never writes one; it publishes an event, and a consumer of the event sends the notifications, in each recipient's language. The module has no translations of its own: the application renders the text.

```go
var TicketAssigned = notification.Category("tickets.assigned") // declared once, as a value

// main
notices := notification.Install(app, users, TicketAssigned /*, ...*/) // users: *identity.Users

// A feature turns its own event into notifications, in a consumer.
app.Events(tickets.Assigned.To("notifications.ticket-assigned",
	func(ctx context.Context, e tickets.Assigned) error {
		return notices.Send(ctx, TicketAssigned, notification.To(e.AssigneeID).Except(e.ActorID),
			func(r notification.Recipient) notification.Message { // once per recipient
				return notification.Message{
					Title: t(r.Locale, "tickets.assigned", e.Title),
					Link:  fmt.Sprintf("/tickets/%d", e.TicketID), // a path inside the app
				}
			})
	}))

// in a test
gocoretest.Publish(t, app, tickets.Assigned, tickets.Assigned{TicketID: 7, AssigneeID: 3})
// GET /v1/notifications as person 3 now has the notification
```

* **`Send` works only inside an event consumer.** The notification's dedupe key is `<event id>:<user>:<category>`, so an event delivered twice, or retried after a failure, makes one notification per person (NOTIF-EVENT-001). Outside a consumer it fails with an error that says to publish an event and send from its consumer. A rolled-back write never notifies, because the event is queued in the write's transaction.
* **Recipients** are `notification.To(ids...)`, optionally `.Except(actor)`. Invalid ids, duplicates and inactive people are dropped (NOTIF-RECIPIENT-001); the accounts are looked up in one call per `Send` (`Users.Find`).
* **The message** is a plain function per recipient (`Recipient` has the id, name and locale). The title is required, text is cut to its columns, the link must be an in-app path (NOTIF-CONTENT-001). A message that can never be valid, or a category that was not registered, sets the event aside as a dead letter at once.
* **Categories** are values, `notification.Category("tickets.assigned")`, optionally `.EmailByDefault()`; they go to `Install` in one list with its options `AppURL`, `TimeZone` and `Enforce`, and `Install` refuses a bad or repeated code or option at start-up with the fix. *Changed in P6: `Category` was a string type; it is now a function that returns a `notification.Kind`, so a category carries its defaults. `Install(app, users, TicketAssigned)` and `Send(ctx, TicketAssigned, ...)` read as before; `Item.Category` is a plain string.*
* **The inbox** (`notices.Inbox`: `List`, `Unread`, `MarkRead`, `MarkAllRead`, `Subscribe`) acts on one person's notifications, whose id comes from the session. Routes under `/v1/notifications`, for a signed-in person and no permission: `GET /` (newest first, `before_id` cursor, `limit` up to 50), `GET /unread`, `POST /:id/read`, `POST /read` (`up_to_id`: only what was seen is marked; 0 marks nothing), `GET /stream` (server-sent events; see below), `POST /test` (sends the signed-in person a test notification through the event queue, category `system.test`).
* **The live stream** tells every open app of a person about a new notification and about a reading, each with the unread count, **after the transaction has committed**, and opens with the current count. It re-checks its session at every heartbeat (25 seconds) and ends when the session was revoked. The `Hub` is process-local: **one instance only**; with several instances an app hears only what its own instance made (the client refetches the count on reconnect).
* **Dead letters**, of every consumer, not only the notification ones: `GET /v1/events/dead-letters` (page, `consumer` filter), `POST /v1/events/dead-letters/:event/:consumer/retry` and `POST /v1/events/dead-letters/retry` (all of one `consumer`), behind the fixed permissions `events.deadletter:view` and `events.deadletter:retry`. Define them on the application's catalogue before `access.Install`: `notification.DefinePermissions(catalogue)`.
* **Tables:** `notifications` (`notification/migrations`), arv-next's table as it is, so an application that has it adopts the file with `MarkApplied`. Tests on SQLite, MySQL and MariaDB 10.3 (`dbtest.Run`).
* The TypeScript contract is `notification.Routes` and `notification.DeadLetterRoutes` (`cmd/modulets` writes `notification/` and `events/`). `cmd/devserver` installs the module with one sample category, two notifications for `user` (one read, one unread) and drains the event queue every second, so the test notification arrives.

---

## Audit Logging

* Tracks entity changes
* Stores before/after state
* Uses reflection-based diff

---

## Multi-Tenancy

* Context-based tenant resolution
* Optional DB query scoping

---

## Rules

* Do not bypass `apperr`
* Do not access DB outside repositories
* Do not put business logic in handlers
* Always pass `context.Context`
* Prefer interfaces over concrete types

---

## Purpose

This repository is a **core library**, not an application.

All business logic should live in consuming services.

---
