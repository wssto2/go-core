package database

import (
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ErrUnsupportedDriver is wrapped by the error for a connection whose driver is neither mysql nor sqlite.
var ErrUnsupportedDriver = errors.New("unsupported driver")

// ErrConnectionNotFound is returned when a named connection has not been registered.
type ErrConnectionNotFound struct {
	Name string
}

func (e ErrConnectionNotFound) Error() string {
	return fmt.Sprintf("database: connection %q not found — was it registered at startup?", e.Name)
}

// ErrInvalidConfig is returned when a ConnectionConfig is missing required fields.
type ErrInvalidConfig struct {
	Name   string
	Reason string
}

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("database: invalid config for connection %q: %s", e.Name, e.Reason)
}

// ErrConnectionFailed is returned when the database driver fails to open.
type ErrConnectionFailed struct {
	Name string
	Err  error
}

func (e ErrConnectionFailed) Error() string {
	return fmt.Sprintf("database: failed to open connection %q: %v", e.Name, e.Err)
}

func (e ErrConnectionFailed) Unwrap() error { return e.Err }

// validateConfig checks that all required fields are present.
func validateConfig(cfg ConnectionConfig) error {
	if cfg.Name == "" {
		return ErrInvalidConfig{Name: "(empty)", Reason: "name is required"}
	}
	driver := cfg.Driver
	if driver == "" {
		driver = DriverMySQL
	}
	switch driver {
	case DriverMySQL:
		if cfg.Host == "" {
			return ErrInvalidConfig{Name: cfg.Name, Reason: "host is required"}
		}
		if cfg.Port == "" {
			return ErrInvalidConfig{Name: cfg.Name, Reason: "port is required"}
		}
		if cfg.Database == "" {
			return ErrInvalidConfig{Name: cfg.Name, Reason: "database is required"}
		}
		if cfg.Username == "" {
			return ErrInvalidConfig{Name: cfg.Name, Reason: "username is required"}
		}
		if cfg.Password == "" {
			return ErrInvalidConfig{Name: cfg.Name, Reason: "password is required"}
		}
	case DriverSQLite:
		if cfg.Database == "" {
			return ErrInvalidConfig{Name: cfg.Name, Reason: "database is required"}
		}
	default:
		reason := fmt.Sprintf("%s %q (supported: %s, %s)", ErrUnsupportedDriver, driver, DriverMySQL, DriverSQLite)

		return ErrInvalidConfig{Name: cfg.Name, Reason: reason}
	}
	return nil
}

// NotFoundAs turns the "no row" error of a read (gorm.ErrRecordNotFound, or sql.ErrNoRows from a raw scan) into
// sentinel, the domain's own not-found error, so a repository returns one line instead of an if:
//
//	if err := r.DB(ctx).First(&row, id).Error; err != nil {
//		return ticket.Ticket{}, database.NotFoundAs(err, ticket.ErrNotFound)
//	}
//
// nil stays nil and any other error passes through unchanged.
func NotFoundAs(err, sentinel error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, sql.ErrNoRows) {
		return sentinel
	}

	return err
}
