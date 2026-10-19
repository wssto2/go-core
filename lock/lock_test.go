//nolint:exhaustruct,exhaustruct_v5,err113,lll,paralleltest,varnamelen // tests in the style of the package's others
package lock_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/lock"
	"github.com/wssto2/go-core/lock/gormstore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	key = "vega-import"
	ttl = 60 * time.Millisecond // a heartbeat every 20 ms of real time
)

// fakeClock is moved by the test; it never reads the wall clock.
type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.at
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.at = c.at.Add(d)
}

// spyStore counts what the heartbeat does and can be told to find the lease gone.
type spyStore struct {
	lock.Store

	refreshes atomic.Int32
	releases  atomic.Int32
	lose      atomic.Bool
}

func (s *spyStore) Refresh(ctx context.Context, k, owner string, until time.Time) (bool, error) {
	s.refreshes.Add(1)

	if s.lose.Load() {
		return false, nil
	}

	return s.Store.Refresh(ctx, k, owner, until) //nolint:wrapcheck // a test double
}

func (s *spyStore) Release(ctx context.Context, k, owner string) error {
	s.releases.Add(1)

	return s.Store.Release(ctx, k, owner) //nolint:wrapcheck // a test double
}

// sharedDB is a SQLite file in the test's folder: the heartbeat runs beside the caller, and an in-memory database
// would vanish with a connection dropped by a cancelled query.
func sharedDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "locks.db")+"?_busy_timeout=5000"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	return db
}

func newLocker(t *testing.T, db *gorm.DB) (*lock.Locker, *spyStore, *fakeClock) {
	t.Helper()

	require.NoError(t, gormstore.Migrate(db))

	spy := &spyStore{Store: gormstore.New(db)}
	clock := &fakeClock{at: time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)}

	return lock.New(spy, clock), spy, clock
}

// waitRefreshes blocks until the heartbeat has refreshed n times in all.
func waitRefreshes(t *testing.T, spy *spyStore, n int32) {
	t.Helper()

	require.Eventually(t, func() bool { return spy.refreshes.Load() >= n }, 5*time.Second, time.Millisecond)
}

func TestHold_SecondIsRefusedWhileTheFirstRefreshesAndSucceedsAfter(t *testing.T) {
	db := sharedDB(t)

	locker, spy, clock := newLocker(t, db)

	running := make(chan struct{})
	finish := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		firstDone <- locker.Hold(t.Context(), key, ttl, func(context.Context) error {
			close(running)
			<-finish

			return nil
		})
	}()

	<-running

	// The clock goes on past the first lease's end, half a ttl at a time; each step waits for a refresh, so the
	// lease is always extended before it can run out.
	for step := range int32(5) {
		clock.Advance(ttl / 2)
		waitRefreshes(t, spy, step+1)

		err := locker.Hold(t.Context(), key, ttl, func(context.Context) error {
			t.Error("the second Hold ran while the first holds the lease")

			return nil
		})
		require.ErrorIs(t, err, lock.ErrHeld)
	}

	close(finish)
	require.NoError(t, <-firstDone)
	assert.EqualValues(t, 1, spy.releases.Load())

	ran := false

	require.NoError(t, locker.Hold(t.Context(), key, ttl, func(context.Context) error {
		ran = true

		return nil
	}))
	assert.True(t, ran, "the lease was free after the first Hold ended")
}

func TestHold_ACancelledContextStopsTheHeartbeatAndReleases(t *testing.T) {
	db := sharedDB(t)

	locker, spy, _ := newLocker(t, db)

	ctx, cancel := context.WithCancel(t.Context())
	running := make(chan struct{})

	done := make(chan error, 1)

	go func() {
		done <- locker.Hold(ctx, key, ttl, func(ctx context.Context) error {
			close(running)
			<-ctx.Done()

			return ctx.Err()
		})
	}()

	<-running
	waitRefreshes(t, spy, 1)
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
	assert.EqualValues(t, 1, spy.releases.Load(), "released although the caller's context ended")

	settled := spy.refreshes.Load()

	time.Sleep(3 * ttl)
	assert.Equal(t, settled, spy.refreshes.Load(), "no refresh after Hold returned")

	got, err := locker.Acquire(t.Context(), key, "someone", ttl)
	require.NoError(t, err)
	assert.True(t, got, "the key is free")
}

func TestHold_ALostLeaseCancelsFn(t *testing.T) {
	db := sharedDB(t)

	locker, spy, _ := newLocker(t, db)
	spy.lose.Store(true)

	err := locker.Hold(t.Context(), key, ttl, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return errors.New("fn was not cancelled")
		}
	})

	require.ErrorIs(t, err, lock.ErrLost)
	require.ErrorIs(t, err, context.Canceled)
}

func TestHold_APanicStillReleases(t *testing.T) {
	db := sharedDB(t)

	locker, spy, _ := newLocker(t, db)

	assert.PanicsWithValue(t, "boom", func() {
		_ = locker.Hold(t.Context(), key, ttl, func(context.Context) error { panic("boom") })
	})
	assert.EqualValues(t, 1, spy.releases.Load())

	require.NoError(t, locker.Hold(t.Context(), key, ttl, func(context.Context) error { return nil }))
}

func TestHold_ReturnsFnsErrorAndRejectsANonPositiveTTL(t *testing.T) {
	db := sharedDB(t)

	locker, _, _ := newLocker(t, db)

	boom := errors.New("boom")
	require.ErrorIs(t, locker.Hold(t.Context(), key, ttl, func(context.Context) error { return boom }), boom)
	require.Error(t, locker.Hold(t.Context(), key, 0, func(context.Context) error { return nil }))
}

func TestLocker_AcquireRefreshReleaseByOwner(t *testing.T) {
	db := sharedDB(t)

	locker, _, clock := newLocker(t, db)
	ctx := t.Context()

	got, err := locker.Acquire(ctx, key, "ana", time.Minute)
	require.NoError(t, err)
	assert.True(t, got)

	got, _ = locker.Acquire(ctx, key, "ivo", time.Minute)
	assert.False(t, got, "refused while live")

	got, _ = locker.Refresh(ctx, key, "ivo", time.Minute)
	assert.False(t, got, "refresh by a stranger")

	clock.Advance(2 * time.Minute)

	got, _ = locker.Acquire(ctx, key, "ivo", time.Minute)
	assert.True(t, got, "taken after expiry")

	require.NoError(t, locker.Release(ctx, key, "ana"))

	got, _ = locker.Refresh(ctx, key, "ivo", time.Minute)
	assert.True(t, got, "a release by a stranger changed nothing")

	require.NoError(t, locker.Release(ctx, key, "ivo"))

	got, _ = locker.Acquire(ctx, key, "ana", time.Minute)
	assert.True(t, got, "free after the owner's release")
}

func TestInstall_RegistersTheTableAndReturnsAWorkingLocker(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	app := gocore.New(bootstrap.DefaultConfig(),
		gocore.WithRegistry(reg), gocore.WithAutoMigrate(t.Context()), gocore.WithLogger(slog.New(slog.DiscardHandler)))

	locker := lock.Install(app)
	require.NotNil(t, app.Database())

	got, err := locker.Acquire(t.Context(), key, "ana", time.Minute)
	require.NoError(t, err)
	assert.True(t, got)
}
