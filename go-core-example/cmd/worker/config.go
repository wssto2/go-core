package main

import (
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
)

func loadConfig() bootstrap.Config {
	var cfg bootstrap.Config
	cfg.AppName = bootstrap.EnvStr("APP_NAME", "go-core-worker")
	cfg.Env = bootstrap.EnvStr("APP_ENV", "development")
	cfg.ShutdownTimeoutSec = bootstrap.EnvInt("SHUTDOWN_TIMEOUT_SEC", 30)

	cfg.Databases = []database.ConnectionConfig{
		{
			Name:            "local",
			Driver:          "mysql",
			Host:            bootstrap.EnvStr("DB_HOST", "127.0.0.1"),
			Port:            bootstrap.EnvStr("DB_PORT", "3306"),
			Database:        bootstrap.EnvStr("DB_DATABASE", "go_core_example"),
			Username:        bootstrap.EnvStr("DB_USERNAME", "root"),
			Password:        bootstrap.EnvStr("DB_PASSWORD", "root"),
			MaxIdleConns:    bootstrap.EnvInt("DB_MAX_IDLE_CONNS", 5),
			MaxOpenConns:    bootstrap.EnvInt("DB_MAX_OPEN_CONNS", 10),
			ConnMaxLifetime: bootstrap.EnvInt("DB_CONN_MAX_LIFETIME_MIN", 5),
		},
	}

	return cfg
}
