package main

import (
	"context"
	"go-core-example/internal/domain/product"
	"go-core-example/internal/middleware"
	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/auth"
	coreauth "github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/frontend"
	"github.com/wssto2/go-core/i18n"
	"github.com/wssto2/go-core/logger"
	"github.com/wssto2/go-core/tenancy"
)

func main() {
	cfg := loadConfig()

	initLogger(cfg)
	initI18n(cfg)

	reg := initDatabase(cfg)

	tokenConfig := coreauth.TokenConfig{
		SecretKey:     cfg.JWT.Secret,
		Issuer:        cfg.JWT.Issuer,
		TokenDuration: cfg.JWT.Duration,
	}

	db := reg.MustGet("local")
	userResolver := middleware.NewUserResolver(db)

	container := bootstrap.NewContainer(reg, "local", tokenConfig, userResolver)

	// -------------------------------------------------------------------------
	// Modules -- add one line here to introduce a new domain.
	// Each module owns its own routes, event subscriptions, and migrations.
	// Nothing else in this file changes when a new domain is added.
	// -------------------------------------------------------------------------
	modules := []bootstrap.Module{
		// domainauth.NewModule(),
		product.NewModule(),
	}

	engine := buildEngine(cfg)

	frontend.MustRegisterSPA(engine, "templates/*.html", frontend.SPAConfig{
		Vite: frontend.ViteConfig{
			Port:       "5173",
			EntryPoint: "main.ts",
			DistDir:    "./static/dist",
		},
		StateBuilder: func(ctx *gin.Context) any {
			return buildAppState(ctx)
		},
	})

	application := bootstrap.NewApp(bootstrap.Config{
		AppName:            cfg.AppName,
		Env:                cfg.Env,
		Port:               cfg.Port,
		ReadTimeoutSec:     cfg.ReadTimeoutSec,
		WriteTimeoutSec:    cfg.WriteTimeoutSec,
		IdleTimeoutSec:     cfg.IdleTimeoutSec,
		ShutdownTimeoutSec: cfg.ShutdownTimeoutSec,
	}, engine)

	protectedMiddleware := []gin.HandlerFunc{
		auth.Authenticated(tokenConfig, userResolver),
		tenancy.FromAuthenticatedUser(),
	}
	application.RegisterModules(engine, container, "api", protectedMiddleware, modules)

	application.AddCloser(func(ctx context.Context) error {
		logger.Log.Info("closing database connections...")
		return reg.CloseAll()
	})

	if err := application.Run(); err != nil {
		logger.Log.Error("application exited with error", "error", err)
		os.Exit(1)
	}
}

func initLogger(cfg AppConfig) {
	if err := logger.Init(logger.Config{
		AppName:    cfg.AppName,
		LogDir:     "logs",
		Env:        cfg.Env,
		Level:      logger.LevelInfo,
		MaxSizeMB:  10,
		MaxBackups: 5,
		MaxAgeDays: 30,
	}); err != nil {
		log.Fatalf("logger: %v", err)
	}
}

func initI18n(cfg AppConfig) {
	if err := i18n.Init(i18n.Config{
		FallbackLang: "en",
		I18nDir:      cfg.I18nDir,
	}); err != nil {
		logger.Log.Warn("i18n init failed, falling back to key strings", "error", err)
	}
}

func initDatabase(cfg AppConfig) *database.Registry {
	return database.NewRegistryFromConfigs(database.RegistryConfig{
		LogLevel:           cfg.Env,
		SlowQueryThreshold: 200 * time.Millisecond,
	}, cfg.Databases)
}

func buildAppState(ctx *gin.Context) any {
	return map[string]any{
		"locale": "en",
	}
}
