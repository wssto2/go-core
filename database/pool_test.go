package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakePool struct {
	idle int
	open int
	lt   time.Duration
}

func (f *fakePool) SetMaxIdleConns(n int)              { f.idle = n }
func (f *fakePool) SetMaxOpenConns(n int)              { f.open = n }
func (f *fakePool) SetConnMaxLifetime(d time.Duration) { f.lt = d }

func TestApplyPoolSettings_AppliesValues(t *testing.T) {
	cfg := ConnectionConfig{MaxIdleConns: 3, MaxOpenConns: 7, ConnMaxLifetime: 2}
	p := &fakePool{}
	ApplyPoolSettings(p, cfg)

	require.Equal(t, 3, p.idle)
	require.Equal(t, 7, p.open)
	require.Equal(t, cfg.connMaxLifetimeDuration(), p.lt)
}

func TestApplyPoolSettings_WithDefaults(t *testing.T) {
	cfg := ConnectionConfig{}.withDefaults()
	p := &fakePool{}
	ApplyPoolSettings(p, cfg)

	require.Equal(t, 5, p.idle)
	require.Equal(t, 75, p.open)
	require.Equal(t, cfg.connMaxLifetimeDuration(), p.lt)
}

func TestApplyPoolSettings_NilSafe(t *testing.T) {
	// should not panic
	ApplyPoolSettings(nil, ConnectionConfig{}.withDefaults())
}
