package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

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

var (
	// Default global logger
	Log *slog.Logger
)

func Init(cfg Config) error {
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
		return err
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

	Log = slog.New(handler)
	slog.SetDefault(Log)

	return nil
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
