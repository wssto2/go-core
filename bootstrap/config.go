package bootstrap

import (
	"time"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/logger"
)

type Config struct {
	AppName string
	Env     string // "production", "development"
	Port    int

	ReadTimeoutSec     int
	WriteTimeoutSec    int
	IdleTimeoutSec     int
	ShutdownTimeoutSec int

	DatabaseRegistry database.RegistryConfig
	Databases        []database.ConnectionConfig

	Log logger.Config

	JWT struct {
		Secret   string
		Issuer   string
		Duration time.Duration
	}

	I18n i18n.Config

	StorageDir string

	CORS struct {
		Origins []string
		Methods []string
		Headers []string
	}
}

func DefaultConfig() Config {
	return Config{
		Port:               8080,
		Env:                "development",
		ReadTimeoutSec:     15,
		WriteTimeoutSec:    15,
		IdleTimeoutSec:     60,
		ShutdownTimeoutSec: 10,
	}
}
