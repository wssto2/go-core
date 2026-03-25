package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

type ctxKey struct{}

type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

type Config struct {
	AppName    string
	LogDir     string // Base directory for logs
	Env        string // "production" or "development"
	Level      LogLevel
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
}

func New(cfg Config) (*slog.Logger, error) {
	if cfg.LogDir == "" {
		cfg.LogDir = "logs"
	}
	if cfg.MaxSizeMB == 0 {
		cfg.MaxSizeMB = 10
	}
	if cfg.MaxBackups == 0 {
		cfg.MaxBackups = 5
	}
	if cfg.MaxAgeDays == 0 {
		cfg.MaxAgeDays = 30
	}

	perm := os.FileMode(0755)
	if err := os.MkdirAll(cfg.LogDir, perm); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	appLogPath := filepath.Join(cfg.LogDir, "app.log")

	// Configure rotation
	fileWriter := &lumberjack.Logger{
		Filename:   appLogPath,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   true,
	}

	opts := &slog.HandlerOptions{
		Level:     parseLevel(cfg.Level),
		AddSource: true,
	}

	var handler slog.Handler

	if cfg.Env != "production" {
		// In dev: write to file AND console, use DEBUG level
		opts.Level = slog.LevelDebug
		multiWriter := io.MultiWriter(fileWriter, os.Stdout)
		handler = slog.NewJSONHandler(multiWriter, opts)
	} else {
		// In prod: write to file only
		handler = slog.NewJSONHandler(fileWriter, opts)
	}

	// Wrap with SourceHandler for context extraction
	handler = NewSourceHandler(handler)

	log := slog.New(handler)
	slog.SetDefault(log)

	return log, nil
}

func MustNew(cfg Config) *slog.Logger {
	log, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func parseLevel(l LogLevel) slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func GetFromContext(ctx context.Context) *slog.Logger {
	if log := ctx.Value(ctxKey{}); log != nil {
		return log.(*slog.Logger)
	}
	return slog.Default()
}
