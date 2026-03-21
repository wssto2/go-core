package main

import (
	"time"

	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
)

type AppConfig struct {
	AppName string
	Env     string
	Port    int

	ReadTimeoutSec     int
	WriteTimeoutSec    int
	IdleTimeoutSec     int
	ShutdownTimeoutSec int

	Databases []database.ConnectionConfig

	JWT struct {
		Secret   string
		Issuer   string
		Duration time.Duration
	}

	I18nDir    string
	StorageDir string

	CORSOrigins []string
}

func loadConfig() AppConfig {
	var cfg AppConfig

	cfg.AppName = bootstrap.EnvStr("APP_NAME", "go-core-example")
	cfg.Env = bootstrap.EnvStr("APP_ENV", "development")
	cfg.Port = bootstrap.EnvInt("APP_PORT", 8080)

	cfg.ReadTimeoutSec = bootstrap.EnvInt("READ_TIMEOUT_SEC", 15)
	cfg.WriteTimeoutSec = bootstrap.EnvInt("WRITE_TIMEOUT_SEC", 15)
	cfg.IdleTimeoutSec = bootstrap.EnvInt("IDLE_TIMEOUT_SEC", 60)
	cfg.ShutdownTimeoutSec = bootstrap.EnvInt("SHUTDOWN_TIMEOUT_SEC", 10)

	// Primary database ("local") -- used by most modules.
	cfg.Databases = []database.ConnectionConfig{
		{
			Name:            "local",
			Driver:          "mysql",
			Host:            bootstrap.EnvStr("DB_HOST", "127.0.0.1"),
			Port:            bootstrap.EnvStr("DB_PORT", "3306"),
			Database:        bootstrap.EnvStr("DB_DATABASE", "go_core_example"),
			Username:        bootstrap.EnvStr("DB_USERNAME", "root"),
			Password:        bootstrap.EnvStr("DB_PASSWORD", "secret"),
			MaxIdleConns:    bootstrap.EnvInt("DB_MAX_IDLE_CONNS", 5),
			MaxOpenConns:    bootstrap.EnvInt("DB_MAX_OPEN_CONNS", 75),
			ConnMaxLifetime: bootstrap.EnvInt("DB_CONN_MAX_LIFETIME_MIN", 5),
		},
		// Second database ("shared") -- available to any module via c.DB("shared").
		// Uncomment and set env vars when a shared/multi-tenant DB is needed.
		// {
		// 	Name:            "shared",
		// 	Driver:          "mysql",
		// 	Host:            envStr("SHARED_DB_HOST", "127.0.0.1"),
		// 	Port:            envStr("SHARED_DB_PORT", "3306"),
		// 	Database:        envStr("SHARED_DB_DATABASE", "go_core_shared"),
		// 	Username:        envStr("SHARED_DB_USERNAME", "root"),
		// 	Password:        envStr("SHARED_DB_PASSWORD", "secret"),
		// 	MaxIdleConns:    envInt("SHARED_DB_MAX_IDLE_CONNS", 5),
		// 	MaxOpenConns:    envInt("SHARED_DB_MAX_OPEN_CONNS", 25),
		// 	ConnMaxLifetime: envInt("SHARED_DB_CONN_MAX_LIFETIME_MIN", 5),
		// },
	}

	cfg.JWT.Secret = bootstrap.EnvStr("JWT_SECRET", "change-me-to-32-bytes-minimum!!")
	cfg.JWT.Issuer = bootstrap.EnvStr("JWT_ISSUER", "go-core-example")
	cfg.JWT.Duration = time.Duration(bootstrap.EnvInt("JWT_DURATION_HOURS", 24)) * time.Hour

	cfg.I18nDir = bootstrap.EnvStr("I18N_DIR", "./i18n")
	cfg.StorageDir = bootstrap.EnvStr("STORAGE_BASE_DIR", "/tmp/go-core-example/uploads")

	cfg.CORSOrigins = []string{bootstrap.EnvStr("CORS_ORIGIN", "http://localhost:3000")}

	return cfg
}
