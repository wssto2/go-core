package database

import "time"

// poolSetter is the minimal interface required to apply connection pool
// settings. *sql.DB implements these methods so it satisfies this interface.
type poolSetter interface {
	SetMaxIdleConns(int)
	SetMaxOpenConns(int)
	SetConnMaxLifetime(time.Duration)
}

// ApplyPoolSettings applies the provided ConnectionConfig pool settings to the
// given poolSetter. It expects cfg to have sensible defaults applied (e.g.
// via ConnectionConfig.withDefaults()).
func ApplyPoolSettings(s poolSetter, cfg ConnectionConfig) {
	if s == nil {
		return
	}
	s.SetMaxIdleConns(cfg.MaxIdleConns)
	s.SetMaxOpenConns(cfg.MaxOpenConns)
	s.SetConnMaxLifetime(cfg.connMaxLifetimeDuration())
}
