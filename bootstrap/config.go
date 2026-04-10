package bootstrap

import "time"

type AppConfig struct {
	Name string `env:"APP_NAME"`

	// Env is the application environment.
	// Common values are "production" and "development".
	Env string `env:"APP_ENV"`
}

type HTTPConfig struct {
	// Port number on which the HTTP server will listen for incoming requests.
	Port int `env:"APP_PORT"`

	// HTTP server timeouts, specified in seconds. These values control how long
	// the server will wait for various operations before timing out.
	ReadTimeout time.Duration `env:"HTTP_READ_TIMEOUT"`

	// Maximum duration before timing out writes of the response. This is
	// important to prevent the server from hanging indefinitely if the client is
	// slow to read the response.
	WriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT"`

	// Maximum amount of time to wait for the next request when keep-alives are
	// enabled. This helps to free up resources for idle connections and can
	// improve the overall performance of the server.
	IdleTimeout time.Duration `env:"HTTP_IDLE_TIMEOUT"`

	// Maximum amount of time to wait for the server to shut down gracefully. This
	// allows the server to finish processing ongoing requests before terminating,
	// which can help prevent data loss and ensure a smooth shutdown.
	ShutdownTimeout time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT"`

	// Amount of time allowed to read request headers. This is important to prevent
	// slow clients from consuming server resources by sending headers very slowly.
	// Setting this timeout can help protect your server from certain types of
	// attacks and improve overall performance.
	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT"`
}

type DatabaseConnectionConfig struct {
	// Name is the key used to retrieve this connection later.
	// e.g. "local", "shared"
	Name string

	// Database driver name (e.g. "postgres", "mysql", "sqlite").
	Driver   string
	Host     string
	Port     string
	Database string
	Username string
	Password string

	// Maximum number of connections in the idle connection
	// pool. Default: 5
	MaxIdleConns int

	// Maximum number of open connections to the database.
	// Default: 75
	MaxOpenConns int

	// Maximum amount of time a connection may be reused in minutes.
	// Default: 5 minutes
	ConnMaxLifetime int

	// Debug enables GORM query logging.
	Debug bool
}

type DatabaseConfig struct {
	Connections []DatabaseConnectionConfig

	// LogLevel controls GORM query logging. "silent", "error", "warn", "info"
	LogLevel string `env:"DATABASE_LOG_LEVEL"`

	// SlowQueryThreshold is the duration above which a query is considered slow.
	SlowQueryThreshold time.Duration `env:"DATABASE_SLOW_QUERY_THRESHOLD"`
}

type LogConfig struct {
	// Dir is the base directory where log files will be stored.
	Dir string `env:"LOG_DIR"`

	// Level is the log level for application logging. This can be set to values
	// like "debug", "info", "warn", "error".
	Level string `env:"LOG_LEVEL"`

	// MaxSize is the maximum size in MB of the log file before it gets rotated.
	MaxSize int `env:"LOG_MAX_SIZE"`

	// MaxBackups is the maximum number of old log files to retain.
	MaxBackups int `env:"LOG_MAX_BACKUPS"`

	// MaxAgeDays is the maximum number of days to retain old log files.
	MaxAgeDays int `env:"LOG_MAX_AGE_DAYS"`
}

type JWTConfig struct {
	// Secret is the secret key used for signing JWT tokens. This should be a
	// strong, random string to ensure the security of your tokens. It is important
	// to keep this value secret and not expose it in your code or version control.
	Secret string `env:"JWT_SECRET"`

	// Issuer is the issuer claim for JWT tokens. This can be used to identify the
	// issuer of the token and can be helpful for validating tokens and ensuring
	// they are coming from a trusted source.
	Issuer string `env:"JWT_ISSUER"`

	// Duration is the duration for which a JWT token is valid, specified in seconds.
	// This determines how long a token can be used before it expires and needs to
	// be refreshed.
	Duration time.Duration `env:"JWT_DURATION"`
}

type FrontendConfig struct {
	// TemplatesPath is the file system path to the directory containing HTML
	// templates. This is used by the application to render dynamic HTML pages.
	// e.g. "templates/*.html"
	TemplatesPath string `env:"TEMPLATES_PATH"`

	// TemplateName is the HTML template to render for all non-API routes.
	// Defaults to "index.html".
	TemplateName string `env:"TEMPLATE_NAME"`

	// StaticPath is the file system path to the directory containing static files
	// such as CSS, JavaScript, and images. This is used by the application to serve
	// static assets to clients. e.g. "static/"
	StaticPath string `env:"STATIC_PATH"`

	// StaticURL is the URL path prefix for serving static files. This is used to
	// define the URL structure for accessing static assets. For example, if you
	// set this to "/static", clients would access static files at URLs
	// like "/static/css/style.css".
	StaticURL string `env:"STATIC_URL"`

	// APIPrefix is the URL prefix that must NOT be caught by NoRoute.
	// Defaults to "/api".
	APIPrefix string `env:"API_PREFIX"`
}

type CORSConfig struct {
	// AllowedOrigins is a slice of allowed origins for Cross-Origin Resource
	// Sharing (CORS). This defines which domains are allowed to make cross-origin
	// requests to your application.
	AllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS"`

	// AllowedMethods is a slice of allowed HTTP methods for CORS requests.
	// This defines which HTTP methods (e.g., GET, POST, PUT, DELETE) are allowed
	// in cross-origin requests.
	AllowedMethods []string `env:"CORS_ALLOWED_METHODS"`

	// AllowedHeaders is a slice of allowed HTTP headers for CORS requests.
	// This defines which headers can be included in cross-origin requests.
	AllowedHeaders []string `env:"CORS_ALLOWED_HEADERS"`
}

type I18nConfig struct {
	// Dir is the directory where internationalization (i18n) files are stored.
	// e.g. "i18n/"
	Dir string `env:"I18N_DIR"`

	// DefaultLocale is the default locale to use when no specific locale is
	// provided by the user.
	DefaultLocale string `env:"I18N_DEFAULT_LOCALE"`
}

type StorageConfig struct {
	// Dir is the directory where uploaded files or other persistent data will be
	// stored. e.g. "storage/"
	Dir string `env:"STORAGE_DIR"`
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
