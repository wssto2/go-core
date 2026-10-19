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

## Getting started

go-core is a library: `go get github.com/wssto2/go-core`, then one `main` that reads the configuration, creates the `*gocore.App`, installs the features and runs it.

**1. A feature is a package with an `Install` function.** Its routes are typed values; `.Requires` names the permission, and a route that says nothing about who may call it stops start-up.

```go
package tickets

var (
    Show   = route.Get[ShowInput, Ticket]("/v1/tickets/:id").Name("tickets.show").Requires(PermView)
    Close  = route.Delete[ShowInput, route.Empty]("/v1/tickets/:id").Requires(PermView).Requires(PermManage) // both must hold
    Routes = route.Group("tickets", Show, Close)
)

func Install(app *gocore.App) {
    service := &Service{db: app.Database(), clock: app.Clock()}

    app.Permissions(Permissions)
    app.Routes(Show.To(service.Show), Close.To(service.Close))
}
```

**2. `main` reads top to bottom.** Configuration is a `bootstrap.Config` (start from `bootstrap.DefaultConfig()` and fill it from the environment with `bootstrap.EnvStr`, `EnvInt`, `EnvBool`, `EnvDuration`); `APP_ENV` is `production`, `staging` (behaves as production) or `development` (also when empty), and any other value stops start-up with a message naming it.

```go
func main() {
    cfg := bootstrap.DefaultConfig()
    cfg.App.Env = bootstrap.EnvStr("APP_ENV", "development")
    cfg.HTTP.TrustedProxies = []string{"10.0.0.0/8"} // who may set X-Forwarded-For and X-Forwarded-Proto

    app := gocore.New(cfg, gocore.WithPrefix("/api"))

    users := identity.Install(app, identity.WithMail(identity.Mail{Sender: sender, CodeSecret: secret}))
    access.Install(app, permissions.All, users)
    tickets.Install(app)

    if err := app.Run(); err != nil { log.Fatal(err) }
}
```

`./myapp migrate` applies the migrations, `./myapp` checks the wiring, then serves. JSON request bodies are capped at 4 MiB (`cfg.HTTP.MaxBodyBytes`; a route that needs more says `.MaxBody(n)`; uploads have their own limit); an id in the path that is not a number answers 400.

**3. Test it with plain calls.** `gocoretest` gives an app on in-memory SQLite and a fixed clock; requests go through the real routes without a port.

```go
app := gocoretest.New(t, gocoretest.SignedIn(authz.User(7, 0)))
tickets.Install(app)

rec := gocoretest.Do(t, app, http.MethodGet, "/v1/tickets/7", nil)
ticket := gocoretest.Decode[Ticket](t, rec)
```

`go-core-example` is a complete application built this way (identity, access, a product catalogue, the SPA shell); "Installing features" below has the details.

---

## Project Structure

### Core Packages

* `gocore` → assembles an application from features: `New`, `Install`-style features, `Later`, `Run` (see "Installing features")
* `route` → typed routes: `route.Get[In, Out](path).Requires(permission)`, binding, validation, permission check
* `gocoretest` → a `gocore.App` for tests: in-memory SQLite, fixed clock, `Do` / `Decode` to call routes
* `bootstrap` → application lifecycle, config, the HTTP engine (what `gocore` is built on)
* `web` → HTTP handling, response format, helpers
* `auth` → authentication (`Provider`, `Authenticated`), the request user (`UserOf`, `UserFromContext`), rate limiting
* `authz` → roles, bindings at a scope, per-record access, delegation, list filtering (see `authz/doc.go`; sub-packages `authzgorm`, `gormstore`, `authzhttp`, `authzts`, `authztest`, `storetest`)
* `database` → repositories, transactions, types
* `validation` → input validation system
* `middlewares` → HTTP middleware (auth, logging, security)
* `worker` → background workers

### Feature Packages

* `access` → roles and access administration over `authz`: role editor, bindings with delegation, effective access, routes and TypeScript contract (see "Roles and access"; core `access/admin`, routes `access/accesshttp`)
* `audit` → audit logging and diff tracking
* `datatable` → filtering, pagination, query helpers
* `tenancy` → multi-tenant context + DB scoping
* `event` → typed events and the per-consumer queue behind them (see "Events"; tables in `event/migrations`)
* `identity` → accounts, password sign-in, sessions, the lock after wrong passwords, the `/auth/me` payload, user administration and each person's profile with e-mail codes (see "Sign-in" and "Users and profile"; core `identity/account`, routes `identity/http`, store `identity/gormstore`, mail text `identity/mailtext`, tests `identity/identitytest`)
* `mail` → the `Sender` port, SMTP over the standard library, inline images, a branded HTML frame (`Brand.Render`), a recording `Sink` for tests, `mail.Redirect` (every mail to one address on a dev copy of production data), per-locale `Renderer` (see "mail")
* `notification` → the in-app inbox, e-mail and web push: categories, `Send` from an event consumer, preferences and quiet hours, the delivery workers, devices, the live stream, the inbox and dead-letter routes (see "Notifications"; migrations in `notification/migrations`, mail text in `notification/mailtext`, the Web Push protocol in `notification/push`, a fake push service for tests in `notification/push/pushtest`)
* `incident` → the incident log: a server error or panic becomes an incident, repeats are counted on it, a person reports a bug, the browser reports its errors, the staff work the list, and the people who hold `incident:manage` are alerted (see "Incidents"; core `incident/core`, store `incident/gormstore`, conformance suite `incident/storetest`, migrations `incident/migrations`, alert wording `incident/mailtext`)
* `lock` → named leases across processes and servers: `Acquire / Refresh / Release` and `Hold(ctx, key, ttl, fn)` with a heartbeat (see "Locks"; store `lock/gormstore`, conformance suite `lock/storetest`, migrations `lock/migrations`)
* `savedview` → a person's saved list views: the filters and search of a list kept under a name and applied again with one click (see "Saved views"; core `savedview/core`, store `savedview/gormstore`, conformance suite `savedview/storetest`, migrations `savedview/migrations`)
* `storage` → named disks of files, private or public, and `web.Serve` to stream one (see "Storage disks"; drivers `storage/local` and `storage/memory`)
* `navigation` → the menu tree an application declares and the filter by held permissions

### Utility Packages

* `apperr` → structured application errors
* `i18n` → localization helpers
* `go2ts` → Go → TypeScript type generation
* `retention` → delete or redact an application's own rows past a window, from rules it owns (`retention.New(db, clock, rules...)`, `Run` with `DryRun` and `Only`)
* `validation` → input validation; `CheckStorable` is the opt-in check for legacy utf8mb3 columns (code `unsupported_characters`)
* `pdf` → document rendering; `pdf/widget` has the paper-form widgets (`SignatureLine`, `LabelWithValue`, `TextLines`, `CodeBoxes`, `YesNo`, `PageBreak`)

### Example App

* `go-core-example` → reference implementation

---

## How It Works

### Request Flow

1. HTTP request enters the engine `gocore` builds
2. Middleware chain executes (`middlewares`)
3. Request is bound by `route` (path, query, JSON body)
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

**Secure by default.** Every route needs an authenticated principal unless it is declared `.Public()`; `.Requires(perm)` adds a permission check on top. A route must say who may call it, or starting the application fails: `.Requires(perm)` (or `RequiresAny`), `.Public()`, `.PersonOnly()` (a person, not a service account) or `.Authenticated()` (any signed-in caller; the handler decides what they may do). The application sets authentication once, with the middleware go-core already has, and non-public routes are mounted behind it:

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

    users := identity.Install(app, identity.WithMail(identity.Mail{Sender: sender, CodeSecret: secret}),
        identity.WithImpersonation(identity.HoldersOf("iam.user:impersonate"))) // sign-in, users, profile, authentication
    access.Install(app, permissions.All, users)               // roles and bindings; builds the engine
    tickets.Install(app, users)

    if err := app.Run(); err != nil { log.Fatal(err) }
}
```

identity needs no engine: `/auth/me` and `HoldersOf` read the application's authorizer (the engine `access.Install` builds) when a request comes, and `*identity.Users` is the `access.SubjectDirectory`. `internal/e2e` installs exactly this and signs in on SQLite, MySQL and MariaDB.

`Install` only collects (`Routes`, `Background`, `Permissions`, `Events`); nothing runs until `Run`, so the order of those never matters.

**A real cycle between two features uses `gocore.Later`:**

```go
reads := gocore.Later[contracts.VehicleReads](app)
vehicles := vehicles.Install(app, reads)
crm := crm.Install(app, vehicles)
reads.Set(crm.VehicleReads) // inside vehicles: reads.Get()
```

**A feature with its own chain: `RoutesWith`** (tested in `gocore/routeswith_test.go`). identity sets the application's one authentication chain (a session). An application whose own routes are signed in another way (API keys, an app token, a per-user rate limit) mounts them behind that chain; the module routes keep identity's:

```go
arv := route.Security{Authenticate: []gin.HandlerFunc{auth.Authenticated(provider)}} // Authorizer: the application's, unless set
app.RoutesWith(arv, pricerule.Routes(svc)...)                                     // 401 and rate limit are the chain's own
```

Public routes stay public, `Requires` is checked, and an undefined permission still stops start-up. A `Security` without `Authenticate` does not fall back to the application's chain. The tenant is not put in the context for you: the chain does that if it wants to.

Checklist for one module:

1. Declare typed routes and a `route.Group` with a request type of the transport: `type CreatePartRequest struct { ThingPath; apppkg.CreatePartInput }`, where `ThingPath` holds the typed ids (`ID int `path:"id"``) and the application's input stays free of HTTP. Embedded structs are bound and validated like `encoding/json` promotes them; the generated TypeScript has the path and query fields flat in the route input and only the body in the schema. No `json:"-"` or text ids needed. An `int` id that does not parse answers 400 (`invalid id`); query and body errors answer 422.
2. Write `Install(app, deps)`: build the service from `deps`, collect `app.Routes` (or `app.RoutesWith` for the application's own chain), `app.Permissions`, `app.Background`.
3. Build the collaborators from `app.Database()` and the other features' services, passed as arguments.
4. `contract.Generate` the feature's TypeScript and compare it with the wire golden before removing the old handlers.
5. For an application's own subject kind (service accounts), answer access screens with `accesshttp.BindingOf`, `RoleSummaryOf`, `RoleOf`, `ScopeOf` and `SubjectAccessOf`.
6. Answer what the old handlers answered, typed: `.Status(http.StatusCreated)` for a create (204 for no body; only 2xx). A paged list is `route.Page[Row]` (byte-identical to `web.Paginated`, `Result.Meta` merged), any other data with a top-level meta is `route.WithMeta[T, M]` (as `web.JSON(c, status, data, meta)`); both combine with `.Status`. Datatable filters are a `Filters url.Values `query:"*" json:"-"`` field (at most one; it takes the parameters no other field declares), read with `datatable.ParamsFromValues`. Several permissions: `.Requires(a).Requires(b)` needs both, `.RequiresAny(a, b)` needs one (all must be in the catalogue); they combine and all must hold (`.Requires(a).RequiresAny(b, c)` is a and (b or c); a refusal names the first unmet `Requires`, else (any-of unmet) the last id of the set, as `RequiresAny` alone does). A response header (`Cache-Control`) is `route.ExchangeOf(ctx).SetHeader(name, value)` (returns an error for `Content-Type`/`Content-Length`; set on success and on error). The request id is `route.ExchangeOf(ctx).RequestID()`; the application's own user is `auth.UserOf[*User](ctx)`, provided its authentication calls `auth.SetUser`. `validation.CheckRules(Input{}, …)` in a test finds a `validation:` rule the validator does not know (`gte=0`); routes refuse one at start-up. None of status, rules or filters is in the generated TypeScript.

**`Run` checks first and says what to fix.** Every problem is collected, each with its fix (`ExampleApp_Check`):

```
gocore: cannot start, 2 problem(s):
  1. database: connection "shared" not found ... (registered connections: local). Fix: register the connection in the database config, or fix its name
  2. gocore.Later[gocore_test.Reads] was never set. Fix: call Set on it once every feature that needs it is installed
```

The check covers an unset `Later`, a declared route with no handler (typed or raw), a `Requires` permission the catalogue does not define, a non-public route with no authentication or a permission route with no authorizer, duplicate or refused routes, and an unknown connection. Then it boots; a failed boot stops what already started, in reverse. Shutdown order: HTTP, background workers, database last.

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
./myapp migrate mark-applied <connection> <version|file>...
                         record migrations as applied without running them (a database whose tables
                         already exist), then exit; every version must be a migration file of the connection
./myapp help             list the commands (an unknown one exits non-zero)
```

Commands never serve or boot modules; they use the database connections and close them. While any migration is pending, starting to serve stops with one start-up problem whose fix is `run "myapp migrate" before starting it`. `app.Migrate(ctx)` is public for tests and for applications with their own command line; `gocoretest.New` migrates as features are installed. `RunCommand(ctx, args, out)` takes the arguments, for tests.

### Commands and the .env file

The application's own commands (a prune, a repair, a one-off import) are added with `app.Command`, from `main` or from a feature's `Install`, so a command may use what the features installed (`gocore/example_test.go`, `go-core-example/cmd/tool`):

```go
app.Command("db:prune", "delete rows past their retention window",
    func(ctx context.Context, app *gocore.App, args []string, out io.Writer) error {
        flags := flag.NewFlagSet("db:prune", flag.ContinueOnError)
        flags.SetOutput(out)
        dryRun := flags.Bool("dry-run", false, "count the rows without deleting them")
        if err := flags.Parse(args); err != nil {
            return err // -h prints the flags and exits 0
        }
        return prune(ctx, app.Database(), *dryRun, out)
    })
```

`./myapp db:prune --dry-run` runs it like `migrate`: the databases are open (`app.Database()`), nothing is checked, served or booted, no worker or `OnStart` runs, and the databases close afterwards. The arguments after the name arrive unparsed; the command parses its own with the standard `flag` package. The error it returns is `Run`'s error, so the process exits non-zero. A name may contain `:` (`iam:check-placement`); an empty one, one with whitespace or a leading `-`, `migrate`, `help` or `ts:generate`, one registered twice and a nil function are App problems, listed by `Check` and `Run`, and a failed `Install` stops a command as it stops serving. `help` lists the built-ins, then the application's commands with their text, sorted; an unknown command's error names them all.

`gocore.LoadEnv(".env")` puts a file of `KEY=value` lines into the process environment, the way `godotenv.Load` does (a variable already set keeps its value; comments, `export`, quotes, `\n` in double quotes and `$NAME` from earlier lines are read). It is a call at the top of `main`, not an option, because the configuration `New` takes is read from the environment before `New` runs. A missing file is an error that is `fs.ErrNotExist`, so `if err := gocore.LoadEnv(".env"); err != nil && !errors.Is(err, fs.ErrNotExist)` makes the file optional and dropping the `errors.Is` makes it required.

### Generating TypeScript

`gocore.WithTypeScript` adds the built-in command `ts:generate`, which writes the frontend's generated sources from the installed application: the route tables of its `route.Group`s, the permission union and metadata of its catalogue, and the entity and Zod schema files for the two lists of Go types the application hands over:

```go
app := gocore.New(cfg, gocore.WithTypeScript(gocore.TS{
    Entities: []any{go2ts.As(dto.TicketResponse{}, "Ticket"), dto.Settings{}}, // go2ts.GenerateTypes' list
    Inputs:   []any{tickets.CreateInput{}},                                     // go2ts.GenerateSchemas' list
    Dir:      "frontend/generated",                                             // the default
}))
```

```
./myapp ts:generate               write routes/, entities/, schemas/ and permissions.ts under Dir
./myapp ts:generate routes        or one part: entities, schemas, permissions, routes
```

`routes/` is removed and written again (`contract.Generate`), `entities/` and `schemas/` go through `go2ts.Sync` (files no longer generated are deleted; an empty list leaves its folder alone), and `permissions.ts` is overwritten. The output is deterministic, so CI regenerates and diffs. The command runs offline: `New` reads the command line (`os.Args[1:]`, or `gocore.WithArgs(...)` in a test) and makes the App `Offline` when it asks for `ts:generate`, so `main` is the same as for serving and needs no database. Every `Install` still runs and still needs the settings it reads, so CI supplies placeholders. Only the application's own parts are written: a go-core module (identity, access, notification, savedview, incident) marks its `Install` with `defer app.Shipping()()`, and the groups of its routes are left out, because `@wssto2/vue-core` ships those route tables (`app.Contracts()` lists all groups, `app.OwnContracts()` the application's: what an audit such as `contract.Wire` walks). The permission union is written from one catalogue (the same one handed to `Permissions` twice, as `access.Install` does, counts once; two different ones stop the command).

All sources of a connection run as one set in version order; two files with one version stop the run and name both. A module's files use its release date as version and never change once tagged.

**Test on every database** (`database/dbtest`). `dbtest.Run(t, func(t *testing.T, db *gorm.DB) { ... })` runs on SQLite always, and on MySQL and MariaDB when `GOCORE_MYSQL_DSN` / `GOCORE_MARIADB_DSN` are set (`root:pw@tcp(127.0.0.1:3310)/`). A server run gets an empty schema it creates and drops itself, so it may run DDL and use several connections; the others skip with a message. `dbtest.RequirePortable(t, migrations.Files)` fails on what MariaDB 10.3 rejects (SKIP LOCKED, FOR UPDATE OF, FOR SHARE, JSON_TABLE, `->>`, RETURNING, window functions). `authztest.MariaDB` uses the same schemas; `AUTHZ_MARIADB_DSN` still works.

### TypeScript contract

A feature's `Routes` group (`route.Group("tickets", ...)`) is its contract. One line per feature generates everything, without building the app or opening a database (`contract/example_test.go`):

```go
contract.Generate("frontend/generated", tickets.Routes, leads.Routes)
// or, from the installed application: contract.Generate(dir, app.Contracts()...)
```

writes `frontend/generated/tickets/{entities,schemas,routes}.ts`: the output types, the input types as Zod schemas (keeping `max:` bounds), and the route table the client builds typed requests from:

```ts
import { route } from "@wssto2/vue-core/client";
export const ticketsRoutes = {
  show: route<ShowInput, Ticket>("GET", "/tickets/:id", { permission: "tickets.ticket:view" }),
  events: route.raw("GET", "/events", { public: true }),
} as const;
```

A route whose output is `datatable.DatatableResult[Row]` types as `ListResult<Row>` (imported from `@wssto2/vue-core/client` next to `route`, with exactly the datatable wire shape); `route.Page[Row]` types as `route<In, Row[], PageMeta>` (`PageMeta` imported from the same module) and `route.WithMeta[T, M]` as `route<In, T, M>` (`M` imported from `./entities`); these route tables need vue-core 0.5.4 or later. Any other generic type is refused. A body field with `json:",omitempty"` is `.optional()` in the Zod schema, unless it is `required`. A typed route's path and its input must agree: every `:name` of the path needs a field tagged `path:"name"` and the other way round, which start-up (`Check`) and `contract.Generate` report with the route, the parameter and the fix, since TypeScript cannot check it.

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

Options, all named: `access.WithRoles(...)` the roles that live in code (computed ones too), `access.WithScopes(hierarchy, catalogue)` places below the root (without it `organization` is the only one), `WithFeatures`, `WithAudit(repo)` (an audit row for every role and binding change, in the same transaction), `WithCache(cache)` (the cache of effective access, to choose its time to live), `On(connection)`. Three hooks carry the application's own rules: `WithRoleCheck(func(ctx, authz.Role) []authz.Problem)` refuses a new or edited custom role as `authz.invalid` (400) with the problem codes, after the catalogue's own validation; `WithBindingCheck(func(ctx, subject, binding) error)` is asked before every bind, unbind and each re-binding of a role replacement; `WithSubjectReach(func(ctx, subject) (bool, error))` says whom the actor may see: a person out of reach answers 404 on their access, scopes, bind and unbind. `compare` takes any two roles the actor may view, predefined or custom. The five permission ids are fixed, so the generated TypeScript always matches.

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

* Session authentication through `identity` (any `auth.Provider` plugs into `auth.Authenticated`)
* Permission-based authorization (`authz`, `.Requires(permission)` on a route)
* Context-based user injection

```go
user, ok := auth.UserOf[MyUser](ctx) // ctx is the request context
```

### Sign-in

`identity` is the sign-in module: accounts, password sign-in, sessions, the lock after wrong passwords, signing in as somebody else, and the `/auth/me` payload the client starts from. One line puts it into an application (`identity/example_test.go`); it also makes it the application's authentication, so every route that is not `.Public()` is behind it and the signed-in person is the authz principal:

```go
users := identity.Install(app, identity.WithMail(identity.Mail{Sender: sender, CodeSecret: secret})) // routes, tables, authentication
```

Mail is the one thing identity cannot default (see "Users and profile" below): pass `WithMail(identity.Mail{Sender: sender, CodeSecret: secret})`, or `identity.WithoutMail()`; without either, start-up stops and says which. It uses the GORM store, bcrypt and its own migrations unless told otherwise. Options are named for what they change: `identity.On(Shared)` (tables on another connection), `WithAccounts(store)` (your own accounts table: identity then creates no `accounts` table, phone column or unique e-mail, only its sign-in, token, code and re-authentication tables), `WithoutUserAdmin()` (no `/v1/iam/users` routes and no `Searcher` needed, for an application with its own users module; `users.Admin()` is nil; the profile, own sign-ins and sessions, login-as and its return stay), `WithAuditRecord("users", map[identity.ChangeAction]string{identity.ChangeCreated: "create", identity.ChangeUpdated: "update", identity.ChangeDeactivated: "delete"})` (the history of accounts is read and written as audit rows of that record type and those verbs, with no row rewritten; where several change actions share one verb a row is read back as the first of created, updated, deactivated, activated, password, email, profile, so the access/details tabs are only as exact as the verbs are distinct), `WithConfig(identity.Config{Locales: []string{"hr", "ba"}})` (change-locale and the admin accept only these; another well-formed tag is `422 identity.locale.unsupported`; `Config.Limits` narrows the field widths of a store with narrower columns), `WithHasher(h)`, `WithRefreshHasher(h)`, `WithAccess(a)` (override where the payload's `authz.MyAccess` comes from; by default the application's authorizer, once `access.Install` is part of it), `WithNavigation(identity.Menu(menu...))` (and the menu, cut to the permissions held), `WithUserProjector(fn)`, `WithImpersonation(identity.HoldersOf("iam.user:impersonate"))` (a rule of your own is an `identity.Impersonation`; checked through the application's authorizer), `WithNotices(n)`, `WithCookies(...)`, `WithPreferences(identity.Preferences{})` (a JSON object per person, see below). `users` is what other features take: `users.Get(ctx, id)`, `users.ChangeLocale(...)`, `users.Sessions(ctx, id)`, `users.RevokeSessions(...)`.

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

**Preferences: a JSON object per person.** `identity.WithPreferences(identity.Preferences{})` keeps what the client remembers between devices (dark mode, the tips a person dismissed). `PUT /v1/auth/preferences` (person only, a service account is refused) takes `{"preferences": {...a part...}}`, merges it into the stored object (RFC 7396: a key sent replaces, a `null` removes, objects merge in depth) and answers `{"preferences": {...the whole object...}}`; the session payload carries the object as a top-level `preferences`, `{}` when nothing is stored. The merged object is capped at 16 KiB (`MaxBytes`; beyond it `422`, field `preferences`, rule `max`). The object is the application's: pass its rules as `Merge func(stored, patch json.RawMessage) (json.RawMessage, error)` and `Validate func(merged json.RawMessage) error` (return `preferences.Invalid("preferences.onboarding.tips", "identifier_format")` or any `*validation.ValidationError` for `422`; any other error is `422` on `preferences`), and a `Store` (`Get(ctx, accountID)`, `Set(ctx, accountID, json)`) over a column the application already has. Without a `Store` the objects live in identity's table `account_preferences` (a goose migration on the connection of identity's other tables; `identity/preferences/storetest` is the conformance suite for your own). Without the option there is no route, no table and no `preferences` key in the payload; the rules are `docs/rules/identity/preferences.md` (IAM-PREF-001). Reading and writing is not atomic, so two patches of one person that arrive together may overwrite each other's keys.

#### Users and profile

The same `Install` serves user administration under `/v1/iam/users` and every signed-in person's own profile under `/v1/iam/profile`; both are in `identity.Routes` (`WithoutUserAdmin()` leaves the first out). Administrators need `iam.user:view` (read) and `iam.user:manage` (write), the ids `access` uses for the same two ideas, and `iam.user.activity:view` (a `System` permission) for a person's activity (`access.Install` defines them in your catalogue; an application without access calls `identity.DefinePermissions(catalogue)`). A profile needs a signed-in person and nothing else.

```go
// sender is a mail.Sender: mail.SMTP(...) in production, mail.NewSink() in a test or a first run.
users := identity.Install(app,
    identity.WithMail(identity.Mail{Sender: sender, CodeSecret: os.Getenv("CODE_SECRET")}), // 32 characters or more; keys the stored one-time codes
    identity.WithDeactivationHook(handOver),           // the application takes part in a deactivation, or refuses it
)

// The first administrator is made in code:
_, err := users.Admin().Create(ctx, account.CreateAccount{
    Login: "admin", Name: "Admin", Email: "admin@example.com", Locale: "en", Password: "a long password",
})
```

Administrators create, update, deactivate and activate people, give them a new password (which lifts their lock and ends their sessions), unlock them, read their sign-in history (`?view=all|failed`, the refused attempts) and the changes made to them (`?view=all|access|details`; access is a new password, a deactivation or an activation), each with its counts in `meta.views` and every row naming who acted as `{id, name}` (null: nobody; an empty name: the account is gone), from one batched lookup per page (go-core's `audit` trail, whose table `identity.Install` registers), and list or end their sessions. What a person *did* is `GET /v1/iam/users/:id/activity?area=&from=&to=` (the audit rows whose actor they are, newest first, each marked with who was signed in as them, with a count per area in `meta.views`): name the areas your records fall in with `identity.WithActivityAreas(identity.Area("crm").Types("customers").Prefix("contracts."))`: the keys are your i18n keys, a record type no area covers is `other`, the changes to accounts are `identity`. `GET /v1/iam/users` answers go-core's datatable page, which the generated TypeScript types as `ListResult<UserRow>` from `@wssto2/vue-core/client`: views `active` (the default), `locked`, `inactive`, `all`, counted under the same `search` in `meta.views` (every list with tabs sends `[{key, count}]`, `datatable.ViewCount`); `search`, `order_col`, `order_dir`, `page`, `per_page`.

A person changes their name and phone, their password (the current one is asked again; five wrong ones lock for fifteen minutes; every other session ends) and their e-mail address: a six-digit code is mailed to the *new* address and the change happens when it is typed in. One account per address (`identity.email.taken`). The code, its limits (60 seconds between sends, five an hour, five attempts, fifteen minutes) and the lock are in `docs/rules/identity/` (IAM-OTP-001 to 004, IAM-REAUTH-001, IAM-PROFILE-001 to 004, IAM-USER-005 to 007).

The text of identity's mails is English (`identity/mailtext`); give `identity.Mail{Content: renderer}` your own in the person's language, for the mails you want to change. `deactivation hooks` run in the transaction of the deactivation before anything is written, and what they write with the context they get commits or rolls back with it:

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

**Inline images and a branded frame.** `Message.Inline` is the parts the HTML shows as `<img src="cid:logo@example.com">`: `mail.Inline{CID, ContentType, Name, Data}`. With them the HTML and the images are a `multipart/related` part of the alternative (text first), so the image shows without the client fetching anything; without them the message is exactly what it was. `Check()` refuses inline parts without HTML, a CID with anything but letters, digits and `. _ ~ + - @`, a repeated CID, a bad content type or a name with a control character, and no data. `Brand` is the look of an application's HTML mails, `Page` what one says; `Render` returns the `Content` with the HTML in the frame, the plain-text alternative and the logo as `Inline`, so `Content.To(...)` (which identity and notification use) carries it:

```go
brand := mail.Brand{Name: "Acme", Logo: &mail.Inline{CID: "logo@acme.example", ContentType: "image/png", Data: logoPNG},
	Color: "#0f766e", Lang: "en", Greeting: "Hello,", Footer: "You get this mail because you have an Acme account."}

c, err := brand.Render(mail.Page{Paragraphs: []string{"Your order 42 is ready."}, Rows: []mail.Row{{Label: "Pickup:", Value: "Main street"}},
	ActionLabel: "Open the order", ActionURL: "https://app.acme.example/orders/42"})
c.Subject = "Order 42"
err = sender.Send(ctx, c.To("ana@example.com"))
```

The zero `Brand` is a plain frame without logo, greeting or footer (colour `#334155`, language `en`). The layout (560 px, Arial, grey page) is fixed; the text and links of a `Page` are escaped, and a `javascript:` link is not made a button.

In a test, `identity.Install` runs on `gocoretest.New(t)`: SQLite tables are created from the models (`app.Schema`), the real SQL files are tested on MySQL and MariaDB. Feature tests that need people use `identitytest.Users(t, identitytest.Account(1, "ana", "secret"))`, which is the same service over memory stores.

---

## Database

* GORM-based repositories
* Transaction support via context
* Custom nullable & typed fields
* SQL migrations per connection (`database/migrate`, goose): `migrations/<connection>/<yyyymmddhhmmss>_<name>.sql`, each database records its own versions; `MarkApplied` adopts a database whose changes ran by hand. `go-core new migration <name> -c <connection>` creates one (the CLI also scaffolds `new feature` and `new event`: `go run github.com/wssto2/go-core/cmd/go-core@vX new …`, versioned with every go-core tag)

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
* A housekeeper, added to the background workers by `app.Events`, deletes events every consumer finished more than 30 days ago (`event.Retention`) with their attempts, in batches by primary key. Dead letters are not deleted by it: their events stay for a retry. To end them after a time, give `gocore.New` the option `gocore.WithDeadLetterRetention(90 * 24 * time.Hour)`: a letter older than that, and the event only it kept, are deleted by that same housekeeper (a host that builds its own `event.Housekeeper` calls `KeepDeadLetters`).
* The claim is MariaDB 10.3 safe (no `SKIP LOCKED`, no `FOR UPDATE OF`: candidates are read, their outbox rows locked by primary key, the consumer's attempts read and the leases written). The queue is tested on SQLite, MySQL and MariaDB 10.3, including three workers on one database never handling an event twice.
* Rows an application wrote with the old `event.InsertOutboxEvent` (removed in v1.21.0) are named by the Go type (`"lead.AssignedEvent"`: package name and type name) with version 1; `event.Define[AssignedEvent]("lead.AssignedEvent")` consumes them unchanged. `outbox_events` has the DDL such an application already has.

---

## Notifications

Notifications: what a person finds when they open the application, read on one device and read on all of them, and, when identity has mail, what is e-mailed to them as they chose. A feature never writes one; it publishes an event, and a consumer of the event sends the notifications, in each recipient's language. The module has no translations of its own: the application renders the text.

```go
var TicketAssigned = notification.Category("tickets.assigned").EmailByDefault() // in-app always; e-mail on until the person turns it off
var TicketCommented = notification.Category("tickets.commented")                // in-app only until the person turns e-mail on

// main: categories and options go in one list
notices := notification.Install(app, users, // users: *identity.Users
	TicketAssigned, TicketCommented,
	notification.AppURL("https://tickets.example.com"), // e-mail links: AppURL + the message's in-app Link
	notification.TimeZone(zagreb),                      // quiet hours are read in it; default time.Local
	// notification.Enforce(func(ctx context.Context, userID int, k notification.Kind, ch notification.Channel) (on, enforced bool, err error) { ... }),
	// notification.WithPush(push.Config{PublicKey: pub, PrivateKey: priv, Subject: "ops@example.com"}), // web push; keys from push.GenerateKeys
)

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

* **`Send` works only inside an event consumer.** The notification's dedupe key is `<event id>:<user>:<category>`, so an event delivered twice, or retried after a failure, makes one notification per person, and one e-mail (NOTIF-EVENT-001). Outside a consumer it fails with an error that says to publish an event and send from its consumer. A rolled-back write never notifies, because the event is queued in the write's transaction.
* **Recipients** are `notification.To(ids...)`, optionally `.Except(actor)`. Invalid ids, duplicates and inactive people are dropped (NOTIF-RECIPIENT-001); the accounts are looked up in one call per `Send` (`Users.Find`).
* **The message** is a plain function per recipient (`Recipient` has the id, name and locale). The title is required, text is cut to its columns, the link must be an in-app path (NOTIF-CONTENT-001). A message that can never be valid, or a category that was not registered, sets the event aside as a dead letter at once.
* **Categories** are values, `notification.Category("tickets.assigned")`, optionally `.EmailByDefault()`, `.PushByDefault()` and `.HighPriority()`; they go to `Install` in one list with its options `AppURL`, `TimeZone`, `Enforce`, `WithPush` and `PushBundleText`, and `Install` refuses a bad or repeated code or option at start-up with the fix. *Changed in P6 (go-core is still at a release candidate): `Category` was a string type; it is now a function that returns a `notification.Kind`, so a category carries its defaults. `Install(app, users, TicketAssigned)` and `Send(ctx, TicketAssigned, ...)` read as before, but `[]notification.Category` is now `[]notification.Option` (or `[]notification.Kind`), `DedupeKey` takes a `Kind`, and `Item.Category` is a plain string.*
* **E-mail goes where identity's mail goes**, so there is nothing more to configure: `identity.WithMail(...)` (`users.Mail()` is its sender and renderer). With `identity.WithoutMail()` e-mail is **unavailable**: the settings say so, no delivery row is ever made and nothing is reported as sent. With mail but no `notification.AppURL(...)`, `Check` and `Run` stop and say so.
* **Preferences** (NOTIF-PREF-001): per person and category, what `Enforce` says (for an application whose administrators decide, such as ARV's dealer policies: it answers `on, enforced`), else the person's own choice, else the category's default; only a choice that differs from the default is stored. In-app is always on, so only e-mail and push are configurable, and `Enforce` is asked for each channel. Routes, for a signed-in person and no permission: `GET /v1/notifications/preferences` (each category with its e-mail and push `{enabled, source}` where the source is `enforced`, `person` or `default`, `email_available`, `push_available`, the quiet hours and the time zone), `PUT /v1/notifications/preferences/:category` with `{"email": true}` and `PUT /v1/notifications/preferences/:category/push` with `{"push": true}` (an enforced setting answers 422 with the reason `notification.setting.enforced`), `PUT /v1/notifications/quiet-hours` with `{"enabled": true, "start": 1260, "end": 420}` (minutes after midnight; start must differ from end). The same in Go: `notices.Settings.Get`, `SetEmail`, `SetPush`, `SetQuietHours`.
* **Quiet hours** (NOTIF-QUIET-001): on by default, 21:00 to 07:00 in `TimeZone`, read on the wall clock, so a daylight-saving night still ends at 07:00. The in-app notification is made at once; the e-mail and the pushes are **held** until the window ends, and then sent only if the notification is still unread (held pushes due together are bundled).
* **Delivery** (NOTIF-DELIVERY-001): `Send` writes an e-mail delivery for each recipient whose setting is on and who has an address, in the transaction that writes the notification. A worker that `Install` starts sends the due ones (claimed with a 5-minute lease, so each is sent once however many instances run; no `SKIP LOCKED`, so MariaDB 10.3 works), retries a failure after 30 seconds, doubling to an hour, for 24 hours, then fails it; a permanent mail error (`mail.ErrPermanent`) fails at once. The e-mail is in the person's language (`en`, `hr`, `bs`, `sl`; `mail.Templates` like identity's), the subject is the title, with the body, a button to `AppURL` plus the link, and a footer that says why they get it and where to change it. The application's own look: give `identity.WithMailContent` a renderer that knows `mailtext.Email` (`notification.email`, data `mailtext.EmailData`). `notices.DeliverDue(ctx)` does one tick of the worker, for a test or a command. Each worker claims only its own channel's deliveries.
* **Web push** (`notification.WithPush(push.Config{PublicKey, PrivateKey, Subject})`; NOTIF-DEVICE-001, NOTIF-DELIVERY-001): people register their browsers as **devices** (`notices.Devices`; routes `GET /v1/notifications/push` for the key to subscribe with, `GET`/`POST /v1/notifications/devices`, `POST .../devices/unsubscribe` for sign-out, `PATCH`/`DELETE .../devices/:id`, `POST .../devices/:id/test`). Registering the same endpoint again is one device, refreshed; an endpoint another person registered moves to the new one and its queued pushes are cancelled. `Send` writes a push delivery per active device of each person whose push setting is on; a `.HighPriority()` category is pushed with `Urgency: high` and lives 4 hours, any other 24; a burst of 3 or more for one device (2 held ones) is bundled into one push (`notification.PushBundleText` writes the text); a worker (`notification.push`, `notices.DeliverPushDue(ctx)` for a tick) retries 401/403/429/5xx and no answer with backoff or `Retry-After`, removes a device the push service says is gone (404/410), and disables one after 5 failures that are its own fault. The protocol is `notification/push`, standard library only: RFC 8291 encryption (proven against the RFC's test vector) and VAPID (RFC 8292); its default client never connects to a private address. `push.GenerateKeys()` makes the key pair once per environment, and it must stay stable. `pushtest.NewService(t)` is a fake push service for an application's tests.
* **The service worker's contract** is the push payload, JSON with the keys `notification_id` (0 for a bundle), `category`, `title`, `body`, `link` (an in-app path), `tag` and `unread_count` (for the icon badge); the application server key it subscribes with is `GET /v1/notifications/push` (`public_key`).
* **Retention**: a worker deletes notifications 90 days after they were made and finished deliveries 30 days after their last change, in batches (`notices.Sweep(ctx)` does it now). Prometheus counters by `category` and `channel` (`in_app`, `email`, `push`) come with it on the app's telemetry: `go_core_notification_{created,held,sent,cancelled}_total` and `go_core_notification_failed_total` (also by `reason`: `gone`, `rejected`, `permanent`, `expired`), see NOTIF-DELIVERY-001 rule 12; the workers `notification.delivery` and `notification.housekeeping` are counted by the worker manager.
* **The inbox** (`notices.Inbox`: `List`, `Unread`, `MarkRead`, `MarkAllRead`, `Subscribe`) acts on one person's notifications, whose id comes from the session. Routes under `/v1/notifications`, for a signed-in person and no permission: `GET /` (newest first, `before_id` cursor, `limit` up to 50), `GET /unread`, `POST /:id/read`, `POST /read` (`up_to_id`: only what was seen is marked; 0 marks nothing), `GET /stream` (server-sent events; see below), `POST /test` (sends the signed-in person a test notification through the event queue, category `system.test`: never e-mailed or held; pushed to every device at once).
* **The live stream** tells every open app of a person about a new notification and about a reading, each with the unread count, **after the transaction has committed**, and opens with the current count. It re-checks its session at every heartbeat (25 seconds) and ends when the session was revoked. The `Hub` is process-local: **one instance only**; with several instances an app hears only what its own instance made (the client refetches the count on reconnect).
* **Dead letters**, of every consumer, not only the notification ones: `GET /v1/events/dead-letters` (page, `consumer` filter), `POST /v1/events/dead-letters/:event/:consumer/retry` and `POST /v1/events/dead-letters/retry` (all of one `consumer`), and `GET /v1/events/consumers` (the consumer names, for the filter: `app.Consumers()`), behind the fixed permissions `events.deadletter:view` and `events.deadletter:retry`. Define them on the application's catalogue before `access.Install`: `notification.DefinePermissions(catalogue)`.
* **Tables:** `notifications`, `notification_deliveries`, `notification_preferences`, `notification_quiet_hours` and `notification_devices` (`notification/migrations`), arv-next's tables as they are, so an application that has them adopts the files with `MarkApplied`. Tests on SQLite, MySQL and MariaDB 10.3 (`dbtest.Run`).
* The TypeScript contract is `notification.Routes` and `notification.DeadLetterRoutes` (`cmd/modulets` writes `notification/` and `events/`). `cmd/devserver` installs the module with one sample category that is e-mailed by default, two notifications for `user` (one read, one unread) and drains the event queue and sends the due e-mails every second, so the test notification arrives; mail goes to an in-memory sink and the log, never out of the machine, and links point at the playground origin. An e-mail as it looks in the sink:

```
Subject: You were assigned Login bug
Hello Ana,

You were assigned Login bug

Fix the sign-in page

Open: https://tickets.example.com/tickets/7

--
You receive this e-mail because e-mail is on for this kind of notification, by default or because you chose it. You can change which notifications are e-mailed to you in your notification settings.
```

---

## Incidents

The incident log of an application: **a server error becomes an incident**, repeats of the same error are grouped under it and counted, a person can **report a bug**, the browser can **report its errors**, the people who run the application work the list, and the ones who hold `incident:manage` are **told of a new incident**. Nothing in the module knows the application: the people come from identity, the alert goes through notifications, the tenant from `tenancy`.

```go
incident.DefinePermissions(catalogue)       // incident:{view,create,update,manage}, only the ones the catalogue lacks; before access.Install

notices := notification.Install(app, users, notification.AppURL("https://app.example.com")) // incident registers its own alert category

incidents := incident.Install(app, users, notices, // users: *identity.Users; notices may be nil when the alerts are off
	incident.Capture{TrailSize: 30, Environment: "production"},   // optional; the defaults are shown (TrailSize 30, Environment from APP_ENV)
	incident.Alerts{Enabled: true},                               // off unless Enabled; Immediate, Cooldown, DigestEvery, Holders are optional
	incident.ClientErrors(ratelimit.NewInMemoryLimiter(20, time.Minute)), // per person; this is the default
	incident.Retain(90*24*time.Hour),                               // occurrences, except the newest of each incident; this is the default
	incident.Link(func(id int) string { return fmt.Sprintf("/incidents/%d", id) }), // where an alert opens; this is the default
)
```

* **Capture** is engine middleware (`app.Use`) that runs before every route. A response of 500 or more from the errors the request collected (the status is `apperr.GetHTTPStatus` of the last one) and a **panic** are recorded; a panic is recovered, recorded and **raised again**, so the 500 the application answers is the one it always answered (`http.ErrAbortHandler` is not a failure). Each capture is a transaction of its own, never the request's, bounded by a timeout, and never fails the request: a capture that fails is logged. The occurrence has the request id, the method, the route pattern, the module (the first path segment after the prefix and the version; `Capture.Module` replaces it), the caller (**a person is recorded as the user, a service account as `principal: {kind: "service", id}` and no user id**: its `GetID()` is 0), the tenant (`tenancy.TenantIDFromContext`), the client address, the query (masked by key, `audit.Mask`), the headers (credentials such as `Authorization`, `Cookie` and `X-Api-Key` redacted, the rest masked the same way) and the caller's **last calls** (INC-TRAIL-001: an in-memory ring of 30 per person or service account, of the calls under `app.Prefix()` that matched a route; the query string is not kept).
* **Grouping** (INC-FINGERPRINT-001): the fingerprint is a SHA-256 of the normalised module, route, error type, message and where it happened (numbers become `{n}` and UUIDs `{uuid}`; a panic is identified by its first three source lines, not by the addresses a stack holds). There is one **open** incident per fingerprint (a unique index on `open_fingerprint`); a repeat counts an occurrence on it, a resolved or ignored one that is seen again is reopened (and not announced as new), and requests that fail together are one incident: a capture that loses a race to create it, or to a deadlock, is tried again. A `Failure` an application finds by itself (a failed job) is `incidents.Service.Capture(ctx, core.Failure{...})`; its `Severity` defaults to high.
* **Routes** (`incident.Routes`, group `incident`, under `/v1/incidents`): `GET /` (a page: `page`, `per_page`, `search`, `order_col`, `order_dir`, filters `status`, `severity`, `source`, `created_at`), `GET /:incidentID` (the incident, with `meta.reports`, `meta.comments` and `meta.watching`), `GET /:incidentID/comments`, `POST /reports` (201, a bug report), `POST /:incidentID/comments` (201), `PATCH /:incidentID` (all `incident:view`, the writes `incident:create` and `incident:update`; the writes are person-only) and `POST /client-errors` (204, any signed-in person). An id that is not a number answers 400. **Who sees what** (INC-CLASSIFY-001 and the visibility rule): a holder of `incident:manage` (the staff) sees every incident, internal comments and every report; anybody else sees the **shared** incidents they reported, wrote a report on or watch, with the public comments and their own reports; a captured incident is staff-only. The reporter and every commenter watch the incident. The people on an incident (`reporter`, `assignee`, an `author`) are `{id, name, email}`, named from `identity` in one call per answer.
* **Bug reports** join the open incident of the same module, page, title and error message (a closed one is reopened). Bounds are `max:` rules on every field, the JSON fields must be JSON (400), and the tables are `utf8mb3`, so a character outside the Basic Multilingual Plane (an emoji) is stored as U+FFFD instead of failing the insert.
* **Browser errors** (`POST /v1/incidents/client-errors`, `{message, stack, route, browser, app_version, request_ids, source, detail}`, every field bounded, at most 20 request ids of 100 characters; `source` (at most 32) is how the page caught the error and `detail` (at most 255) the application's label for where, both optional, kept in the occurrence's context, left out when empty and never part of the grouping): source `client_error`, severity **low**, staff-only, grouped by the message and the **top stack frame** (`at fn (file:1:2)` or `fn@file:1:2`; line and column numbers are normalised away); the request ids link the error to the server incident of a call that failed. Only a signed-in **person** reports (401 without a session, 403 for a service account). A person's reports are limited (default 20 a minute); the ones over the limit are **dropped with the same 204**: a typed route cannot answer 429, and the browser has nothing to do about it. An incident seen by more than one source is `mixed`.
* **Alerts** (off unless `Alerts.Enabled`): a new incident publishes `incident.opened` (`incident.Opened`, payload `incident.NewIncident`) **in the transaction that creates it**, whether or not alerts are on, so an application can consume it. With alerts on, a consumer sends at once, as a high priority notification of `incident.AlertCategory` (inbox, and e-mail unless the person turned it off), a new incident of an **immediate severity** (default critical and high) when no alert of that severity was sent within the **cooldown** (default 5 minutes); every other new incident waits in `incident_digest_queue`. A worker (default every 30 minutes) takes the waiting ones out of the queue **in the transaction that queues one `incident.digest` event**, and its consumer sends them as a single notification (a title with the count and a line per incident). **The cooldown starts only after a send that succeeded** (a send that fails is retried by the event queue and holds nothing back). The wording is in `incident/mailtext` in English, Croatian, Bosnian and Slovenian, in the person's language; `incidents.FlushDigest(ctx)` sends the digest now.
* **Who is alerted**: the people who hold `incident:manage` at any place. `Alerts.Holders` says who; the default is `authz.Engine.HoldersOf(ctx, "incident:manage")` on the application's authorizer (the access module's engine: the subjects bound to a predefined, computed or custom role that grants it, each once; service accounts are returned with their kind and left out here). Another authorizer needs `Alerts.Holders`. Nobody holding the permission drops the alert with a warning and starts no cooldown.
* **Housekeeping**: a worker (every hour; `incidents.Sweep(ctx)` does it now) deletes the occurrences older than the retention (default 90 days) except the **newest of each incident**, and the digest items that went out a day ago, in batches by primary key.
* **Tables** (`incident/migrations`): `incidents`, `incident_occurrences`, `incident_reports`, `incident_comments`, `incident_watchers`, `incident_alert_cooldowns` and `incident_digest_queue`. The first seven files are the tables an application that grew them as `bug_incidents`, `bug_occurrences`, `bug_reports`, `bug_comments`, `bug_report_watchers`, `incident_notification_cooldowns` and `incident_notification_digest_queue` has once it renames them (and `dealer_id` of the occurrences to `tenant_id`): it adopts them with `MarkApplied`. Two more files add `client_error` to the source enum and `digested_at` to the digest queue. Tests: the store conformance suite `incident/storetest` and the service on SQLite, MySQL and MariaDB 10.3 (`dbtest.Run`), concurrent captures included.
* The TypeScript contract is `incident.Routes` (`cmd/modulets` writes `incident/`).

---

## Saved views

A person keeps the **filters and search of a list under a name** and applies them again with one click. Nothing in the module knows the application or its lists: a list is a key the screen chooses, and the saved state is whatever the screen sends.

```go
savedview.Install(app) // the table and three routes; no options, no permission
```

* **Routes** (`savedview.Routes`, group `savedview`, under `/v1/saved-views`, every one person-only, so a service account is refused with 403): `GET ?list=` (the person's views of that list, by name), `POST` (save one) and `DELETE /:id` (204). A view is `{id, list_key, name, state, updated_at}` with `state` the JSON object that was saved, never a string; the owner is not on the wire. An id that is not a number answers 400, a zero or negative one 422.
* **The rule (SAVEDVIEW-001)**: a view is private to the person. The person comes from the session and never from the request, every read and write is keyed on it, and another person's view cannot be listed, replaced or deleted: deleting it is a 404, the same as a missing id. Saving under a name the person already uses on that list **replaces** that view's state. A person keeps **at most 20 views per list**: a new name beyond that is a 400 (`at most 20 saved views per list`); replacing one is still allowed. The state is opaque to the module but must be a **JSON object** (`state must be a JSON object`, 400), at most 4000 characters; `list_key` is required, at most 100, `name` required, at most 60, both trimmed. The limits are fixed.
* **Table** (`savedview/migrations`): `saved_views` with `user_id`, `list_key`, `name`, `state`, `created_at`, `updated_at`, a unique index on `(user_id, list_key, name)` and one on `(user_id, list_key)`; there is no tenant column. The file is the DDL an application that grew the table as `saved_filters` has once it renames the table and its two indexes (`saved_views_user_list_name`, `saved_views_user_list`): it adopts it with `MarkApplied`. Tests: the store conformance suite `savedview/storetest` on SQLite, MySQL and MariaDB 10.3 (`dbtest.Run`), the service, and the routes' status, body and refusals.
* The TypeScript contract is `savedview.Routes` (`cmd/modulets` writes `savedview/`).

---

## Locks

`lock` is a **named lease with an owner and a time to live**: "only one of these runs at a time" across processes and servers, kept in the database they share.

```go
locker := lock.Install(app) // the table locks (a migration) and a Locker over app.Database()

err := locker.Hold(ctx, "vega-import", 30*time.Minute, func(ctx context.Context) error {
	return runImport(ctx) // ctx ends if the lease is lost
})
if errors.Is(err, lock.ErrHeld) { /* another run holds it: skip */ }
```

* **Calls:** `Acquire(ctx, key, owner, ttl)` takes a key that is free, expired or already the owner's; `Refresh` renews the owner's lease; `Release` gives it back, only for its owner. `Hold` does all three around a function: it refreshes every ttl/3 while the function runs, cancels its context when a refresh finds the lease taken (`ErrLost`, joined with the function's own error), stops the heartbeat and then releases, also on a panic or a cancelled context; `ErrHeld` (wrapped, naming the key) when someone else holds it. The time comes from `app.Clock()`; the heartbeat ticks in real time. Servers sharing a key should agree on the time to within a fraction of the ttl.
* **Store and table:** `lock.Store` is the port; `lock/gormstore` is the SQL store on `locks (lock_key, owner, lease_until)` (`lock/migrations`), an `UPDATE ... WHERE owner = me OR lease_until < now` then an `INSERT` that reads a duplicate key as "not taken", so it runs on MariaDB 10.3 and MySQL; `lock/storetest` is the conformance suite for your own. `lock.Install(app, conn...)` registers the schema on the primary connection or the one named.
* **Next to it:** `worker.Every(name, interval, fn)` is a `Worker` that calls `fn` at once and every interval until the context ends (an error is logged, never fatal) - `app.Background(worker.Every(...))`; wrap `fn` in `Hold` for a job that must run on one server only. `database.CreateUnlessDuplicate(db, value)` inserts and reports whether it did, a unique-key refusal being `false` and not an error.

---

## Storage disks

An application keeps its files on **named disks**. A disk is **private** unless it says `Public`: a private disk's files are opened only through the application, after the route's own permission check; a public disk's files are at a base URL (a path, later possibly a CDN) and anyone with the link can open one. The application passes the root and the base URL from its own configuration; go-core reads none.

```go
app.Disks(
    storage.Disk("uploads").Local("storage/uploads"),                 // private: served only through the app
    storage.Disk("public").Local("storage/public").Public("/media"),  // anyone with the URL can open it
)

documents := storage.Use(app, "uploads").Folder("vehicle-documents")
photos    := storage.Use(app, "public").Folder("vehicles")

documents.Put(ctx, name, file, size, mime)
web.Serve(c, documents, name, web.Attachment(doc.Name))  // Range, Last-Modified, Content-Disposition with filename*
url, _ := photos.URL(ctx, "123/photos/front.jpg")        // "/media/vehicles/123/photos/front.jpg"
```

* **`Folder(prefix)`** scopes a disk (folders nest): nothing outside the prefix can be read, listed or deleted through the result, and a key that escapes it (`..`, an absolute path, a backslash) is refused.
* **Start-up**: an unknown name given to `storage.Use`, a name declared twice, a disk with no `Local`/`Memory`/`Driver`, or a root that cannot be created stops `Run` and `Check` with the other problems. Declare the disks before the features that use them.
* **`URL`** is an error on a private disk. **`Delete`** of a missing file is not an error. **`Put`** writes the whole stream and replaces an existing key; a failed write leaves no partial file and keeps the old one. `size` is the length the stream must have, `0` means unknown, and a stream of another length is refused, never cut. `Exists` is there for the application's own collision rule.
* **`web.Serve(c, disk, key, disposition)`** streams with `http.ServeContent` when the driver can open a file for seeking (`storage.Opener`; `local` and `memory` can), so `Range`, `If-Modified-Since` and `Last-Modified` work; any other driver is streamed whole. A missing file is the standard JSON 404. `web.Attachment(name)` and `web.Inline(name)` build the disposition. Serve checks no permission: the route does, which is what makes a private disk private.
* **`local`** resolves its root, refuses a symlinked file or directory inside it, and writes through a temporary file renamed into place. **`memory`** has the same capabilities, for tests.

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
