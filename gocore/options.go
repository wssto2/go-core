package gocore

import (
	"regexp"
	"strings"
	"time"

	"github.com/wssto2/go-core/frontend"
	"github.com/wssto2/go-core/ratelimit"
)

// spaOption is what WithSPA or WithSPAConfig asked for.
type spaOption struct {
	state  frontend.AppStateBuilder
	config *frontend.SPAConfig
}

// WithSPA serves the single-page application's shell for every path that is not a route, with state rendered into it
// as appState (state may be nil). The templates, the Vite manifest and the assets are found by convention under
// ./frontend; WithSPAConfig names them. The API prefix (WithPrefix) is the one path the shell leaves alone.
func WithSPA(state frontend.AppStateBuilder) Option {
	return func(app *App) { app.spa = &spaOption{state: state} }
}

// WithSPAConfig serves the single-page application with an explicit frontend configuration. The configuration's
// APIPrefix defaults to the prefix of WithPrefix, and DevMode to the development environment.
func WithSPAConfig(cfg frontend.SPAConfig) Option {
	return func(app *App) { app.spa = &spaOption{config: &cfg} }
}

// WithRateLimit limits requests with two limiters; either may be nil.
//
// perUser budgets each signed-in user, keyed by user ID once auth.Authenticated has resolved the identity (a limiter
// that runs before the route groups' authentication never sees the user). Routes that are not authenticated are not
// counted by it.
//
// perIP limits requests per client IP on the API: paths under the prefix of WithPrefix. The SPA shell and static
// assets are left alone, so a limited browser still loads the page instead of a JSON 429 body. Size it for a whole
// office behind one NAT: it guards against a single flooding machine and brute force on anonymous endpoints.
//
//	gocore.WithRateLimit(ratelimit.NewInMemoryLimiter(300, time.Minute), ratelimit.NewInMemoryLimiter(3000, time.Minute))
func WithRateLimit(perUser, perIP ratelimit.Limiter) Option {
	return func(app *App) { app.perUserLimit, app.perIPLimit = perUser, perIP }
}

// WithGlobalRateLimit counts ALL requests in one bucket shared by the whole server: once it is exhausted every caller
// receives 429 until the window resets, legitimate users included. It is not a DDoS defence (volumetric attacks are
// the CDN's or the load balancer's); use it for cost control on an API that charges per call, or to protect one
// backend of known capacity.
func WithGlobalRateLimit(l ratelimit.Limiter) Option {
	return func(app *App) { app.globalLimit = l }
}

// WithIdempotency deduplicates requests that carry an Idempotency-Key header: requests sharing a key run the handler
// once; concurrent duplicates wait for the first and receive its captured response, and later duplicates within ttl
// replay it. Requests without the header are unaffected, so it is safe to apply to every route: clients opt in per
// request.
func WithIdempotency(ttl time.Duration) Option {
	return func(app *App) { app.idempotencyTTL = ttl }
}

// WithTrustedOrigins lists origins that may load resources from this app (img-src) and embed it in iframes
// (frame-ancestors). Pass full URLs or bare origins: only the scheme and host are used; empty ones are ignored.
func WithTrustedOrigins(origins ...string) Option {
	return func(app *App) { app.trustedOrigins = append(app.trustedOrigins, origins...) }
}

type declaredDatabase struct {
	name    string
	maxOpen int
}

// DatabaseOption adjusts a connection declared with WithDatabase.
type DatabaseOption func(*declaredDatabase)

// DefaultMaxOpenConns is the pool size used when <NAME>_DB_MAX_OPEN_CONNS is not set (75 otherwise).
func DefaultMaxOpenConns(n int) DatabaseOption {
	return func(d *declaredDatabase) { d.maxOpen = n }
}

var databaseName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// WithDatabase declares a connection by name and reads it from the environment under that name's prefix:
// WithDatabase("local") reads LOCAL_DB_HOST, _PORT, _DATABASE, _USERNAME, _PASSWORD, _DRIVER (default mysql), _SQL_MODE,
// _CHARSET, _MAX_IDLE_CONNS (5), _MAX_OPEN_CONNS (75) and _CONN_MAX_LIFETIME_MIN (5). The first connection declared is
// the primary, the one App.Database returns without an argument. There is no implicit DB_* connection: an application
// that declares none has no database.
func WithDatabase(name string, opts ...DatabaseOption) Option {
	return func(app *App) {
		d := declaredDatabase{name: name, maxOpen: defaultMaxOpenConns}
		for _, o := range opts {
			o(&d)
		}

		switch {
		case !databaseName.MatchString(name):
			app.fail("WithDatabase was given the name "+quote(name), "a connection name is letters, digits and underscores, such as \"local\": it names the "+strings.ToUpper(name)+"_DB_* variables")
		case app.hasDatabase(name):
			app.fail("the connection "+quote(name)+" is declared twice", "declare each connection once")
		default:
			app.databases = append(app.databases, d)
		}
	}
}

func quote(s string) string { return `"` + s + `"` }

func (a *App) hasDatabase(name string) bool {
	for _, d := range a.databases {
		if d.name == name {
			return true
		}
	}

	return false
}

const (
	defaultMaxOpenConns = 75
	defaultMaxIdleConns = 5
	defaultLifetimeMin  = 5
)

// readConfig reads Config from the environment. The names are the ones an application's .env already carries.
func (a *App) readConfig() Config {
	e, cfg := a.env, DefaultConfig()
	sec := func(name string, def time.Duration) time.Duration {
		return time.Duration(e.Int(name, int(def/time.Second))) * time.Second
	}

	cfg.App.Name = e.String("APP_NAME", cfg.App.Name)
	cfg.App.Env = e.String("APP_ENV", cfg.App.Env)
	cfg.HTTP.Port = e.Int("APP_PORT", cfg.HTTP.Port)
	cfg.HTTP.ReadTimeout = sec("READ_TIMEOUT_SEC", cfg.HTTP.ReadTimeout)
	cfg.HTTP.WriteTimeout = sec("WRITE_TIMEOUT_SEC", cfg.HTTP.WriteTimeout)
	cfg.HTTP.IdleTimeout = sec("IDLE_TIMEOUT_SEC", cfg.HTTP.IdleTimeout)
	cfg.HTTP.ShutdownTimeout = sec("SHUTDOWN_TIMEOUT_SEC", cfg.HTTP.ShutdownTimeout)
	cfg.HTTP.MaxBodyBytes = int64(e.Int("HTTP_MAX_BODY_BYTES", 0))
	cfg.HTTP.TrustedProxies = e.List("TRUSTED_PROXIES")

	cfg.Database.LogLevel = e.String("DATABASE_LOG_LEVEL", cfg.Database.LogLevel)
	cfg.Database.LogParameters = e.Bool("DATABASE_LOG_PARAMETERS", false)

	cfg.Log.Dir = e.String("LOG_DIR", cfg.Log.Dir)
	cfg.Log.Level = e.String("LOG_LEVEL", cfg.Log.Level)
	cfg.Log.MaxSize = e.Int("LOG_MAX_SIZE_MB", cfg.Log.MaxSize)
	cfg.Log.MaxBackups = e.Int("LOG_MAX_BACKUPS", cfg.Log.MaxBackups)
	cfg.Log.MaxAgeDays = e.Int("LOG_MAX_AGE_DAYS", cfg.Log.MaxAgeDays)

	cfg.I18n.Dir = e.String("I18N_DIR", cfg.I18n.Dir)
	cfg.I18n.DefaultLocale = e.String("I18N_FALLBACK_LANG", "en")

	cfg.Frontend.StaticPath = e.String("STATIC_PATH", cfg.Frontend.StaticPath)
	cfg.Frontend.StaticURL = e.String("STATIC_URL", cfg.Frontend.StaticURL)

	return cfg
}

// readConnections reads the connections declared with WithDatabase, in declaration order.
func (a *App) readConnections() []DatabaseConnectionConfig {
	e := a.env

	out := make([]DatabaseConnectionConfig, 0, len(a.databases))
	for _, d := range a.databases {
		p := strings.ToUpper(d.name) + "_DB_"

		out = append(out, DatabaseConnectionConfig{
			Name:            d.name,
			Driver:          e.String(p+"DRIVER", "mysql"),
			Host:            e.String(p+"HOST", ""),
			Port:            e.String(p+"PORT", ""),
			Database:        e.String(p+"DATABASE", ""),
			Username:        e.String(p+"USERNAME", ""),
			Password:        e.String(p+"PASSWORD", ""),
			SQLMode:         e.String(p+"SQL_MODE", ""),
			Charset:         e.String(p+"CHARSET", ""),
			MaxIdleConns:    e.Int(p+"MAX_IDLE_CONNS", defaultMaxIdleConns),
			MaxOpenConns:    e.Int(p+"MAX_OPEN_CONNS", d.maxOpen),
			ConnMaxLifetime: time.Duration(e.Int(p+"CONN_MAX_LIFETIME_MIN", defaultLifetimeMin)) * time.Minute,
		})
	}

	return out
}
