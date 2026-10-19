package bootstrap

import (
	"fmt"
	"time"
)

type AppConfig struct {
	Name string

	// Env is the application environment: "production", "staging" or
	// "development"; empty means development. Staging behaves exactly as
	// production (CSP, error bodies, HSTS); any other value is a start-up
	// problem (see Validate).
	Env string
}

// The values of AppConfig.Env.
const (
	EnvProduction  = "production"
	EnvStaging     = "staging"
	EnvDevelopment = "development"
)

// Development reports whether the application runs in development: Env is
// "development" or empty. Everything else, including an unknown value that
// Validate reports, gets the production behaviour, so a typo never loosens
// the CSP or exposes debug error bodies.
func (a AppConfig) Development() bool {
	return a.Env == "" || a.Env == EnvDevelopment
}

// Validate reports an Env that is none of the three values, naming it.
func (a AppConfig) Validate() error {
	switch a.Env {
	case "", EnvDevelopment, EnvStaging, EnvProduction:
		return nil
	}

	return fmt.Errorf("APP_ENV %q is not one of %s, %s, %s", a.Env, EnvProduction, EnvStaging, EnvDevelopment)
}

type HTTPConfig struct {
	// Port number on which the HTTP server will listen for incoming requests.
	Port int

	// HTTP server timeouts, specified in seconds. These values control how long
	// the server will wait for various operations before timing out.
	ReadTimeout time.Duration

	// Maximum duration before timing out writes of the response. This is
	// important to prevent the server from hanging indefinitely if the client is
	// slow to read the response.
	WriteTimeout time.Duration

	// Maximum amount of time to wait for the next request when keep-alives are
	// enabled. This helps to free up resources for idle connections and can
	// improve the overall performance of the server.
	IdleTimeout time.Duration

	// Maximum amount of time to wait for the server to shut down gracefully. This
	// allows the server to finish processing ongoing requests before terminating,
	// which can help prevent data loss and ensure a smooth shutdown.
	ShutdownTimeout time.Duration

	// How long Shutdown lets in-flight requests finish on their own before it
	// cancels every request context. Long-lived requests (Server-Sent Events,
	// long polling) only end when their context ends, so without this they
	// would hold Shutdown for the whole ShutdownTimeout. Normal requests that
	// finish within the grace period are drained untouched; one still running
	// after it sees its context cancelled. Capped at ShutdownTimeout.
	ShutdownGracePeriod time.Duration

	// Amount of time allowed to read request headers. This is important to prevent
	// slow clients from consuming server resources by sending headers very slowly.
	// Setting this timeout can help protect your server from certain types of
	// attacks and improve overall performance.
	ReadHeaderTimeout time.Duration

	// MaxBodyBytes is the largest JSON (non-multipart) request body, in bytes. Zero means
	// DefaultMaxBodyBytes; negative sets no limit. A route that needs more says so with
	// Route.MaxBody; multipart uploads have their own limit (web/upload).
	MaxBodyBytes int64

	// TrustedProxies controls which reverse proxies are allowed to supply
	// X-Forwarded-For / X-Forwarded-Proto headers. Safe default is nil, which
	// disables proxy trust entirely.
	TrustedProxies []string
}

// DefaultMaxBodyBytes is the JSON body limit when HTTPConfig.MaxBodyBytes is zero: 4 MiB.
const DefaultMaxBodyBytes int64 = 4 << 20

// BodyLimit is MaxBodyBytes with its default applied; 0 or less means no limit.
func (c HTTPConfig) BodyLimit() int64 {
	if c.MaxBodyBytes == 0 {
		return DefaultMaxBodyBytes
	}

	return max(c.MaxBodyBytes, 0)
}

// Defaults applied when ShutdownTimeout / ShutdownGracePeriod are not set.
const (
	defaultShutdownTimeout     = 10 * time.Second
	defaultShutdownGracePeriod = 3 * time.Second
)

// shutdownTimeout is ShutdownTimeout, or its default when unset.
func (c HTTPConfig) shutdownTimeout() time.Duration {
	if c.ShutdownTimeout <= 0 {
		return defaultShutdownTimeout
	}

	return c.ShutdownTimeout
}

// shutdownGracePeriod is ShutdownGracePeriod (or its default when unset),
// never longer than the shutdown timeout.
func (c HTTPConfig) shutdownGracePeriod() time.Duration {
	grace := c.ShutdownGracePeriod
	if grace <= 0 {
		grace = defaultShutdownGracePeriod
	}

	return min(grace, c.shutdownTimeout())
}

type DatabaseConnectionConfig struct {
	// Name is the key used to retrieve this connection later.
	// e.g. "local", "shared"
	Name string

	// Database driver name (currently "mysql" or "sqlite").
	Driver   string
	Host     string
	Port     string
	Database string
	Username string
	Password string

	// SQLMode overrides the MySQL sql_mode for every session opened by this pool.
	// Useful when connecting to servers with different default modes
	// (e.g. MySQL 5.1 in production vs MySQL 8 locally).
	//
	// Provide a comma-separated list of mode names, or leave empty to use the
	// server's default. Example for local MySQL 8 with a legacy schema:
	//
	//   "ALLOW_INVALID_DATES,NO_ENGINE_SUBSTITUTION"
	SQLMode string

	// Charset overrides the connection charset in the MySQL DSN.
	// Defaults to "utf8mb4" when empty, which requires MySQL >= 5.5.3.
	// Set to "utf8" when connecting to older MySQL servers (< 5.5.3) that do
	// not support utf8mb4.
	Charset string

	// Maximum number of connections in the idle connection
	// pool. Default: 5
	MaxIdleConns int

	// Maximum number of open connections to the database.
	// Default: 75
	MaxOpenConns int

	// Maximum amount of time a connection may be reused.
	// Default: 5 minutes
	ConnMaxLifetime time.Duration

	// Debug enables GORM query logging.
	Debug bool
}

type DatabaseConfig struct {
	Connections []DatabaseConnectionConfig

	// LogLevel controls GORM query logging. "silent", "error", "warn", "info"
	LogLevel string

	// SlowQueryThreshold is the duration above which a query is considered slow.
	SlowQueryThreshold time.Duration

	// LogParameters controls whether actual query parameter values are shown in
	// logged SQL. Defaults to false (values hidden with ? placeholders). Set to
	// true only in non-production environments via DATABASE_LOG_PARAMETERS=true.
	LogParameters bool
}

type LogConfig struct {
	// Dir is the base directory where log files will be stored.
	Dir string

	// Level is the log level for application logging. This can be set to values
	// like "debug", "info", "warn", "error".
	Level string

	// MaxSize is the maximum size in MB of the log file before it gets rotated.
	MaxSize int

	// MaxBackups is the maximum number of old log files to retain.
	MaxBackups int

	// MaxAgeDays is the maximum number of days to retain old log files.
	MaxAgeDays int
}

type JWTConfig struct {
	// Secret is the secret key used for signing JWT tokens. This should be a
	// strong, random string to ensure the security of your tokens. It is important
	// to keep this value secret and not expose it in your code or version control.
	Secret string

	// Issuer is the issuer claim for JWT tokens. This can be used to identify the
	// issuer of the token and can be helpful for validating tokens and ensuring
	// they are coming from a trusted source.
	Issuer string

	// Duration is the duration for which a JWT token is valid, specified in seconds.
	// This determines how long a token can be used before it expires and needs to
	// be refreshed.
	Duration time.Duration
}

type FrontendConfig struct {
	// TemplatesPath is the file system path to the directory containing HTML
	// templates. This is used by the application to render dynamic HTML pages.
	// e.g. "templates/*.html"
	TemplatesPath string

	// TemplateName is the HTML template to render for all non-API routes.
	// Defaults to "index.html".
	TemplateName string

	// StaticPath is the file system path to the directory containing static files
	// such as CSS, JavaScript, and images. This is used by the application to serve
	// static assets to clients. e.g. "static/"
	StaticPath string

	// StaticURL is the URL path prefix for serving static files. This is used to
	// define the URL structure for accessing static assets. For example, if you
	// set this to "/static", clients would access static files at URLs
	// like "/static/css/style.css".
	StaticURL string

	// APIPrefix is the URL prefix that must NOT be caught by NoRoute.
	// Defaults to "/api".
	APIPrefix string

	// EntryScript is the path to the frontend entry point file (e.g. "src/main.js").
	EntryScript string

	// ManifestPath is the path to the Vite manifest file generated during the
	// build process (e.g. "./frontend/dist/.vite/manifest.json"). This file
	// contains mappings of original asset names to their hashed versions, which
	// is essential for cache busting in production.
	ManifestPath string

	// AssetsURLPrefix is the URL prefix for serving frontend assets. This should
	// match the public path used in your Vite configuration (e.g. "/frontend/dist").
	// This is used to correctly resolve asset URLs in the rendered HTML templates.
	AssetsURLPrefix string
}

type CORSConfig struct {
	// AllowedOrigins is a slice of allowed origins for Cross-Origin Resource
	// Sharing (CORS). This defines which domains are allowed to make cross-origin
	// requests to your application.
	AllowedOrigins []string

	// AllowedMethods is a slice of allowed HTTP methods for CORS requests.
	// This defines which HTTP methods (e.g., GET, POST, PUT, DELETE) are allowed
	// in cross-origin requests.
	AllowedMethods []string

	// AllowedHeaders is a slice of allowed HTTP headers for CORS requests.
	// This defines which headers can be included in cross-origin requests.
	AllowedHeaders []string
}

type I18nConfig struct {
	// Dir is the directory where internationalization (i18n) files are stored.
	// e.g. "i18n/"
	Dir string

	// DefaultLocale is the default locale to use when no specific locale is
	// provided by the user.
	DefaultLocale string
}

type StorageConfig struct {
	// Dir is the directory where uploaded files or other persistent data will be
	// stored. e.g. "storage/"
	Dir string
}

// Config holds the configuration settings for the application.
type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Database DatabaseConfig
	Log      LogConfig
	JWT      JWTConfig
	Frontend FrontendConfig
	CORS     CORSConfig
	I18n     I18nConfig
	Storage  StorageConfig
}

// DefaultConfig returns a Config struct with default values.
func DefaultConfig() Config {
	return Config{
		App: AppConfig{
			Env: "development",
		},
		HTTP: HTTPConfig{
			Port:              8080, //nolint:mnd
			ReadTimeout:       5 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       120 * time.Second,
			ShutdownTimeout:   15 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
		},
		Database: DatabaseConfig{
			LogLevel:           "info",
			SlowQueryThreshold: 1 * time.Second,
		},
		Log: LogConfig{
			Dir:        "logs",
			Level:      "info",
			MaxSize:    100, //nolint:mnd
			MaxBackups: 7,   //nolint:mnd
			MaxAgeDays: 30,  //nolint:mnd
		},
		Frontend: FrontendConfig{
			TemplatesPath: "templates/*.html",
			TemplateName:  "index.html",
			StaticPath:    "static/",
			StaticURL:     "/static",
			APIPrefix:     "/api",
		},
		CORS: CORSConfig{
			AllowedOrigins: []string{},
			AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"},
		},
		Storage: StorageConfig{
			Dir: "storage/",
		},
	}
}
