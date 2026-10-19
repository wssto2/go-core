// Package lock is a named lease with an owner and a time to live: "only one of these runs at a time" across
// processes and servers, kept in the database both already share.
//
//	locker := lock.Install(app) // the table locks and a Locker over app.Database()
//
//	err := locker.Hold(ctx, "vega-import", 30*time.Minute, func(ctx context.Context) error {
//		return runImport(ctx) // ctx ends if the lease is lost
//	})
//	if errors.Is(err, lock.ErrHeld) { /* another run is going: skip this one */ }
//
// A lease is a row (key, owner, lease_until). Acquire takes a key that is free, expired or already the owner's;
// Refresh pushes the lease out while the work goes on; Release gives it back, only if the owner still holds it.
// A holder that dies is not waited for forever: its lease runs out and the next Acquire takes the key.
// Hold does all three around a function: it refreshes every ttl/3 while run runs, cancels run's context if a
// refresh finds the lease gone (the time between a refresh and its expiry is the most a lost lease can go
// unnoticed, so choose a ttl longer than the longest pause a holder can have), stops the heartbeat and then
// releases, also when run panics or its context is cancelled.
//
// The time comes from the Clock (app.Clock() in Install), never from time.Now, so tests can move it. The lease
// compares the clock of whoever asks, so the servers that share a key should agree to within a fraction of the ttl.
// The heartbeat itself ticks in real time.
//
// The store is a port: lock/gormstore is the SQL one (MariaDB 10.3 and MySQL safe), lock/storetest the
// conformance suite for your own.
package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/lock/gormstore"
	"github.com/wssto2/go-core/lock/migrations"
)

// ErrHeld is returned (wrapped, naming the key) by Hold when the lease belongs to someone else.
var ErrHeld = errors.New("lock: held by someone else")

// ErrLost is returned (wrapped, naming the key) by Hold when the lease was lost while run ran: a refresh found it
// taken by another owner, or could not reach the store until the lease had run out. run's context was cancelled.
var ErrLost = errors.New("lock: lease lost")

// releaseTimeout bounds the release that follows run, which must run even when the caller's context has ended.
const releaseTimeout = 10 * time.Second

const (
	// beatsPerLease is how many times a lease is refreshed within its ttl.
	beatsPerLease = 3
	// maxHost keeps the owner name within its column (100) next to the random suffix.
	maxHost = 60
)

// Clock tells the time; gocore.Clock has the same method.
type Clock interface {
	Now() time.Time
}

// Store keeps the leases. Every method acts on one key and is atomic in the store.
type Store interface {
	// Acquire takes the key for owner until `until` when it is free, its lease ended before now, or owner already
	// holds it, and reports whether owner holds it afterwards.
	Acquire(ctx context.Context, key, owner string, now, until time.Time) (bool, error)
	// Refresh moves the lease of the key to `until` when owner holds it (even past its end, if nobody took it
	// since), and reports whether it did.
	Refresh(ctx context.Context, key, owner string, until time.Time) (bool, error)
	// Release deletes the key when owner holds it; it is not an error when someone else does or nobody.
	Release(ctx context.Context, key, owner string) error
}

// Locker takes and holds leases in a Store.
type Locker struct {
	store Store
	clock Clock
}

// New returns a Locker over store, reading the time from clock.
func New(store Store, clock Clock) *Locker { return &Locker{store: store, clock: clock} }

// Install puts the table locks into app and returns a Locker over the primary connection, or the one named.
// Nothing is routed or started.
func Install(app *gocore.App, conn ...database.Connection) *Locker {
	app.Schema(gocore.Schema{Files: migrations.Files, Models: gormstore.Migrate}, conn...)

	return New(gormstore.New(app.Database(conn...)), app.Clock())
}

// Acquire takes the key for owner for ttl and reports whether owner holds it: it is free, expired, or already
// owner's (then the lease is renewed). Owners must be unique per holder; two runs sharing an owner share the lease.
func (l *Locker) Acquire(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	now := l.clock.Now()

	return l.store.Acquire(ctx, key, owner, now, now.Add(ttl)) //nolint:wrapcheck // the store names the operation
}

// Refresh renews owner's lease for ttl from now and reports whether owner still holds the key.
func (l *Locker) Refresh(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	return l.store.Refresh(ctx, key, owner, l.clock.Now().Add(ttl)) //nolint:wrapcheck // the store names the operation
}

// Release gives the key back, only when owner holds it.
func (l *Locker) Release(ctx context.Context, key, owner string) error {
	return l.store.Release(ctx, key, owner) //nolint:wrapcheck // the store names the operation
}

// Hold runs run while holding the key for a holder of its own. It returns an error wrapping ErrHeld, without
// running run, when someone else holds the key. While run runs the lease is refreshed every ttl/3; if that finds
// the lease lost, run's context is cancelled and Hold returns ErrLost joined with run's own error. The lease is
// released after run returns, with the heartbeat already stopped, on every exit including a panic.
func (l *Locker) Hold(ctx context.Context, key string, ttl time.Duration, run func(ctx context.Context) error) error {
	if ttl <= 0 {
		return fmt.Errorf("lock: ttl must be positive, got %s", ttl) //nolint:err113 // a caller bug, named once
	}

	owner := newOwner()

	got, err := l.Acquire(ctx, key, owner, ttl)
	if err != nil {
		return err
	}

	if !got {
		return fmt.Errorf("%w: %q", ErrHeld, key)
	}

	runCtx, cancel := context.WithCancel(ctx)

	var lost atomic.Bool

	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		l.heartbeat(runCtx, cancel, &lost, key, owner, ttl)
	}()

	defer func() {
		cancel()
		<-stopped // no refresh can follow the release

		relCtx, relCancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer relCancel()

		err := l.Release(relCtx, key, owner)
		if err != nil {
			slog.WarnContext(ctx, "lock: release failed, the lease ends on its own", "key", key, "error", err)
		}
	}()

	err = run(runCtx)
	if lost.Load() {
		return errors.Join(fmt.Errorf("%w: %q", ErrLost, key), err)
	}

	return err
}

// heartbeat refreshes the lease every ttl/3 until ctx ends. A refresh that finds the lease taken, or fails until
// the lease has run out here, marks it lost and cancels run's context.
func (l *Locker) heartbeat(
	ctx context.Context, cancel context.CancelFunc, lost *atomic.Bool, key, owner string, ttl time.Duration,
) {
	ticker := time.NewTicker(max(ttl/beatsPerLease, time.Nanosecond))
	defer ticker.Stop()

	until := l.clock.Now().Add(ttl)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		now := l.clock.Now()

		held, err := l.Refresh(ctx, key, owner, ttl)

		switch {
		case ctx.Err() != nil:
			return
		case err != nil && now.Before(until):
			slog.WarnContext(ctx, "lock: refresh failed, trying again", "key", key, "error", err)
		case err != nil || !held:
			lost.Store(true)
			cancel()

			return
		default:
			until = now.Add(ttl)
		}
	}
}

// newOwner is a holder name no other run shares: the host and a random suffix, for whoever reads the table.
func newOwner() string {
	host, _ := os.Hostname()
	host = host[:min(len(host), maxHost)]

	var b [8]byte

	_, _ = rand.Read(b[:])

	return host + "-" + hex.EncodeToString(b[:])
}
