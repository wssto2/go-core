package main

import (
	"time"

	"github.com/wssto2/go-core/bootstrap"
)

func loadConfig() bootstrap.Config {
	cfg := bootstrap.DefaultConfig()
	cfg.App.Name = bootstrap.EnvStr("APP_NAME", "go-core-worker")
	cfg.App.Env = bootstrap.EnvStr("APP_ENV", "development")
	cfg.HTTP.ShutdownTimeout = time.Duration(bootstrap.EnvInt("SHUTDOWN_TIMEOUT_SEC", 30)) * time.Second

	cfg.Database.Connections = []bootstrap.DatabaseConnectionConfig{
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
