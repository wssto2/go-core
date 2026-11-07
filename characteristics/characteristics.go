// Package characteristics is the editor-maintained reference data of a project ("Karakteristike": news categories,
// brands, delivery countries): a tree of a few levels in the project's own tables, read and written from a description
// the project writes in code, with the routes the administration screen and the forms' selects use.
//
//	tree := characteristics.Default(2) // or a description of tables the project already has
//	characteristics.Install(app, permissions, tree, characteristics.WithMigrations(2), characteristics.WithoutUsage())
//
// The module does not own the tables. The description (core.Tree) names each level's table and columns and what the
// extra columns mean (label, type, shown in the list, editable, for the whole level or one branch). A column it does
// not name is never read into the API or written, so site-builder's and arv-next's tables are served as they are. An
// invalid description stops start-up and names every problem; once the migrations ran, so does a described column
// the database lacks. Default(n) describes the ready-made tables of WithMigrations(n), up to three levels
// (characteristics_level1 to 3); a project adds its own columns in its own migration and appends their core.Field to
// the levels it got.
//
// Permissions (CHARACTERISTICS-PERM-001): characteristics:view reads the tree, characteristics:edit changes it. They
// are meant for the webmaster role only: Install defines them in the catalogue, and the application gives them to the
// role (Grants), or to a computed role that holds the whole catalogue. The form fields' options route needs no
// permission, only a signed-in caller. Labels holds their texts in Croatian and English for the application's i18n
// files.
//
// Where an item is used is the application's to say (core.Usage): Install requires WithUsage or WithoutUsage, so not
// knowing is a choice and not an accident. Without a port the answers say "usage unknown" and a delete is not blocked
// by use. The read tree is kept for a minute (WithCache) and dropped on every write through the module.
//
//nolint:cyclop,exhaustruct,exhaustruct_v5,funlen // Install is a flat list of checks
package characteristics

import (
	"errors"
	"time"

	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/characteristics/core"
	"github.com/wssto2/go-core/characteristics/gormstore"
	"github.com/wssto2/go-core/characteristics/migrations"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"gorm.io/gorm"
)

// The permissions of the module. The ids are fixed.
const (
	// View reads the tree, an item and where it is used.
	View = "characteristics:view"
	// Edit creates, changes, switches, orders and deletes items. It needs View.
	Edit = "characteristics:edit"
)

// Labels are the texts of the permissions' label keys, for the application's i18n files.
//
//nolint:gochecknoglobals // constant texts
var Labels = map[string]core.Text{
	"perm.characteristics.view": {HR: "Pregled karakteristika", EN: "View characteristics"},
	"perm.characteristics.edit": {HR: "Uređivanje karakteristika", EN: "Edit characteristics"},
}

// Grants are the grants of both permissions, for the predefined webmaster role:
//
//	authz.Role{Key: "webmaster", Name: "Webmaster", Grants: append(otherGrants, characteristics.Grants()...)}
func Grants() []authz.Grant { return authz.Grants(authz.QualifierAll, View, Edit) }

// Default describes the first n (1 to 3) ready-made tables; see core.DefaultTree.
func Default(n int) core.Tree { return core.DefaultTree(n) }

// Install puts the module into app: the routes, the permissions and, with WithMigrations, the ready-made tables. The
// catalogue is the application's permission catalogue, before access.Install builds the engine from it. After a
// problem Run lists, the value is nil.
func Install(app *gocore.App, catalogue *authz.Catalogue, tree core.Tree, opts ...Option) *core.Service {
	defer app.Shipping()()

	cfg := settings{ttl: time.Minute}
	for _, opt := range opts {
		opt(&cfg)
	}

	failed := false
	fail := func(what, fix string) {
		app.Fail(what, fix)

		failed = true
	}

	prepared, err := tree.Prepare()
	if err != nil {
		if described, ok := errors.AsType[*core.DescriptionError](err); ok {
			for _, problem := range described.Problems {
				fail("characteristics description: "+problem, "fix the core.Tree given to characteristics.Install")
			}
		}
	}

	switch {
	case catalogue == nil:
		fail("characteristics needs the permission catalogue",
			"pass it as the second argument: characteristics.Install(app, catalogue, tree)")
	case !cfg.usageChosen:
		fail("characteristics needs to know where items are used",
			"pass characteristics.WithUsage(port) to refuse deleting what is used, "+
				"or characteristics.WithoutUsage() to say the application cannot tell")
	case cfg.migrate < 0 || cfg.migrate > migrations.Levels:
		fail("characteristics.WithMigrations takes 1 to 3 levels", "pass the number of levels the project uses, at most 3")
	}

	if failed {
		return nil
	}

	err = definePermissions(catalogue)
	if err != nil {
		app.Fail("characteristics cannot define its permissions: "+err.Error(),
			"define them in the catalogue before the module that builds the engine")

		return nil
	}

	app.Permissions(catalogue)

	conn := app.Database(cfg.conn...)

	if n := cfg.migrate; n > 0 {
		app.Schema(gocore.Schema{
			Files:  migrations.First(n),
			Models: func(db *gorm.DB) error { return gormstore.Migrate(db, n) },
		}, cfg.conn...)
	}

	store := gormstore.New(conn, prepared)
	app.OnStart(store.Check)

	service := core.New(prepared, store, core.Config{Usage: cfg.usage, Clock: app.Clock(), CacheTTL: cfg.ttl})

	app.Routes(Declare().To(service)...)

	return service
}

// definePermissions (CHARACTERISTICS-PERM-001) adds the module's permissions the catalogue lacks.
func definePermissions(catalogue *authz.Catalogue) error {
	for _, def := range []struct {
		id   string
		opts []authz.DefineOption
	}{
		{View, []authz.DefineOption{authz.Label("perm.characteristics.view")}},
		{Edit, []authz.DefineOption{authz.Label("perm.characteristics.edit"), authz.Requires(View)}},
	} {
		if _, ok := catalogue.Lookup(def.id); ok {
			continue
		}

		err := catalogue.Define(def.id, def.opts...)
		if err != nil {
			return err //nolint:wrapcheck // named by the caller
		}
	}

	return nil
}

type settings struct {
	usage       core.Usage
	usageChosen bool
	ttl         time.Duration
	migrate     int
	conn        []database.Connection
}

// Option configures Install.
type Option func(*settings)

// WithUsage says where items are used: a delete of a used item is refused (409) unless forced.
func WithUsage(usage core.Usage) Option {
	return func(s *settings) { s.usage, s.usageChosen = usage, usage != nil }
}

// WithoutUsage says the application cannot tell where items are used. Answers say "usage unknown" and a delete is not
// blocked by use (it still is by the items below it).
func WithoutUsage() Option { return func(s *settings) { s.usage, s.usageChosen = nil, true } }

// WithCache sets how long the read tree is kept (default one minute; zero keeps nothing). A write through the module
// drops it at once; a write by another process, such as the legacy application during a migration, shows after it.
func WithCache(ttl time.Duration) Option { return func(s *settings) { s.ttl = ttl } }

// WithMigrations registers the ready-made tables of the first n levels (1 to 3). Without it the module creates no
// table: the project's own tables are the ones described.
func WithMigrations(n int) Option { return func(s *settings) { s.migrate = n } }

// On puts the tables on another connection than the primary.
func On(conn database.Connection) Option {
	return func(s *settings) { s.conn = []database.Connection{conn} }
}
