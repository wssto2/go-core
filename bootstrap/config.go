package bootstrap

import (
	"time"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/engine"
	"github.com/wssto2/go-core/frontend"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/logger"
)

type Config struct {
	AppName string `env:"APP_NAME"`
	Env     string `env:"APP_ENV"` // "production", "development"
	Port    int    `env:"APP_PORT"`

	ReadTimeoutSec        int `env:"HTTP_READ_TIMEOUT_SEC"`
	WriteTimeoutSec       int `env:"HTTP_WRITE_TIMEOUT_SEC"`
	IdleTimeoutSec        int `env:"HTTP_IDLE_TIMEOUT_SEC"`
	ShutdownTimeoutSec    int `env:"HTTP_SHUTDOWN_TIMEOUT_SEC"`
	ReadHeaderTimeoutSec  int `env:"HTTP_READ_HEADER_TIMEOUT_SEC"`

	DatabaseRegistry database.RegistryConfig
	Databases        []database.ConnectionConfig

	Log logger.Config

	JWT struct {
		Secret   string        `env:"JWT_SECRET" validation:"required"`
		Issuer   string        `env:"JWT_ISSUER"`
		Duration time.Duration `env:"JWT_DURATION"`
	}

	I18n i18n.Config

	StorageDir string `env:"STORAGE_DIR"`

	Engine engine.Config

	SPA frontend.SPAConfig
}

func DefaultConfig() Config {
	return Config{
		Port:               8080,
		Env:                "development",
		ReadTimeoutSec:        15,
		WriteTimeoutSec:       15,
		IdleTimeoutSec:        60,
		ShutdownTimeoutSec:    10,
		ReadHeaderTimeoutSec:  10,
	}
}
