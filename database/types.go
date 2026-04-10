package database

import "time"

// ConnectionConfig holds everything needed to open one database connection pool.
// Applications build these at startup and register them with the Registry.
//
//	database.ConnectionConfig{
//	    Name:            "local",
//	    Host:            os.Getenv("LOCAL_DB_HOST"),
//	    Port:            os.Getenv("LOCAL_DB_PORT"),
//	    Database:        os.Getenv("LOCAL_DB_DATABASE"),
//	    Username:        os.Getenv("LOCAL_DB_USERNAME"),
//	    Password:        os.Getenv("LOCAL_DB_PASSWORD"),
//	}
type ConnectionConfig struct {
	// Name is the key used to retrieve this connection later.
	// e.g. "local", "shared", "etx_hr"
	Name string

	// Driver is the database driver name (e.g. "postgres", "mysql", "sqlite").
	Driver string

	Host     string
	Port     string
	Database string
	Username string
	Password string

	// Pool settings — zero values use the defaults below.
	MaxIdleConns    int // default: 5
	MaxOpenConns    int // default: 75
	ConnMaxLifetime int // minutes, default: 5

	// Debug enables GORM query logging.
	Debug bool
}

// withDefaults returns a copy of the config with pool defaults applied.
func (c ConnectionConfig) withDefaults() ConnectionConfig {
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 5
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 75
	}
	if c.ConnMaxLifetime == 0 {
		c.ConnMaxLifetime = 5
	}
	return c
}

func (c ConnectionConfig) connMaxLifetimeDuration() time.Duration {
	return time.Duration(c.ConnMaxLifetime) * time.Minute
}

// RegistryConfig holds the options used when creating a Registry.
type RegistryConfig struct {
	// LogLevel controls GORM query logging.
	// "silent", "error", "warn", "info" — defaults to "error".
	LogLevel string

	// SlowQueryThreshold is the duration above which a query is considered slow.
	// Default: 1 second.
	SlowQueryThreshold time.Duration
}

func (c RegistryConfig) withDefaults() RegistryConfig {
	if c.LogLevel == "" {
		c.LogLevel = "error"
	}
	if c.SlowQueryThreshold == 0 {
		c.SlowQueryThreshold = time.Second
	}
	return c
}

// DB type constants — used by custom GORM types to return the correct SQL type
// for the target database engine.
const (
	Null = "null"

	DriverSQLite = "sqlite"
	DriverMySQL  = "mysql"

	SQLiteInt      = "integer"
	MySQLInt       = "int"
	SQLiteString   = "text"
	MySQLString    = "varchar"
	SQLiteFloat    = "decimal(10,2)"
	MySQLFloat     = "decimal(10,2) unsigned"
	SQLiteDate     = "date"
	MySQLDate      = "date"
	SQLiteDateTime = "datetime"
	MySQLDateTime  = "datetime"
	SQLiteBool     = "boolean"
	MySQLBool      = "tinyint(1) unsigned"
	SQLiteJSON     = "json"
	MySQLJSON      = "json"
)
