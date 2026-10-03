package account_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

func hashOf(t *testing.T, k identitytest.Kit, id int) string {
	t.Helper()

	a, err := k.Users.Get(t.Context(), id)
	require.NoError(t, err)

	return a.PasswordHash
}

// IAM-REAUTH-001.2: a right password is a match; a wrong one is ErrWrongPassword, not a lock.
func TestConfirmChecksThePassword(t *testing.T) {
	k := seeded(t)

	require.NoError(t, k.Reauth.Confirm(t.Context(), 1, hashOf(t, k, 1), "secret"))
	require.ErrorIs(t, k.Reauth.Confirm(t.Context(), 1, hashOf(t, k, 1), "nope"), account.ErrWrongPassword)
}

// IAM-REAUTH-001.3: the 5th attempt locks, and while locked even the right password is not checked.
func TestTheFifthAttemptLocksAndALockedAccountIsNotChecked(t *testing.T) {
	counter := &countingHasher{PasswordHasher: identitytest.Hasher}
	k := seeded(t, identitytest.WithHasher(counter))
	hash := hashOf(t, k, 1)
	before := counter.matches

	for range 4 {
		require.ErrorIs(t, k.Reauth.Confirm(t.Context(), 1, hash, "nope"), account.ErrWrongPassword)
	}

	err := k.Reauth.Confirm(t.Context(), 1, hash, "nope")
	require.True(t, apperr.HasReason(err, account.ReasonReauthLocked), "the 5th wrong password is already the lock")
	require.Equal(t, before+5, counter.matches, "the 5th was still checked")

	var ae *apperr.AppError
	require.ErrorAs(t, err, &ae)
	require.Equal(t, 900, ae.Params["retry_after"])
	require.Equal(t, identitytest.Epoch.Add(15*time.Minute).Format(time.RFC3339), ae.Params["locked_until"])

	err = k.Reauth.Confirm(t.Context(), 1, hash, "secret")
	require.True(t, apperr.HasReason(err, account.ReasonReauthLocked), "the right password does not get in while locked")
	require.Equal(t, before+5, counter.matches, "and it was not checked")

	require.NoError(t, k.Reauth.Confirm(t.Context(), 2, hashOf(t, k, 2), "hunter2"), "another account is not locked")
}

// IAM-REAUTH-001.3: the 5th password, when right, lifts the lock at once.
func TestTheFifthAttemptWhenRightLiftsTheLock(t *testing.T) {
	k := seeded(t)
	hash := hashOf(t, k, 1)

	for range 4 {
		require.ErrorIs(t, k.Reauth.Confirm(t.Context(), 1, hash, "nope"), account.ErrWrongPassword)
	}

	require.NoError(t, k.Reauth.Confirm(t.Context(), 1, hash, "secret"))
	require.NoError(t, k.Reauth.Confirm(t.Context(), 1, hash, "secret"), "not locked afterwards")
}

// IAM-REAUTH-001.2: a correct password clears the count.
func TestACorrectPasswordClearsTheCount(t *testing.T) {
	k := seeded(t)
	hash := hashOf(t, k, 1)

	for range 3 {
		require.ErrorIs(t, k.Reauth.Confirm(t.Context(), 1, hash, "nope"), account.ErrWrongPassword)
	}

	require.NoError(t, k.Reauth.Confirm(t.Context(), 1, hash, "secret"))

	for range 4 {
		require.ErrorIs(t, k.Reauth.Confirm(t.Context(), 1, hash, "nope"), account.ErrWrongPassword, "a fresh count of four is no lock")
	}
}

// IAM-REAUTH-001.5: the lock ends by itself after 15 minutes and a fresh count starts.
func TestTheLockEndsByItself(t *testing.T) {
	k := seeded(t)
	hash := hashOf(t, k, 1)

	for range 5 {
		_ = k.Reauth.Confirm(t.Context(), 1, hash, "nope")
	}

	k.Clock.Advance(15*time.Minute - time.Second)
	require.True(t, apperr.HasReason(k.Reauth.Confirm(t.Context(), 1, hash, "secret"), account.ReasonReauthLocked))

	k.Clock.Advance(time.Second)
	require.ErrorIs(t, k.Reauth.Confirm(t.Context(), 1, hash, "nope"), account.ErrWrongPassword, "a fresh count: one attempt")
	require.NoError(t, k.Reauth.Confirm(t.Context(), 1, hash, "secret"))
}

// IAM-REAUTH-001.6: a burst of parallel guesses gets exactly 5 password checks.
func TestAParallelBurstGetsExactlyFiveChecks(t *testing.T) {
	counter := &countingHasher{PasswordHasher: identitytest.Hasher}
	k := seeded(t, identitytest.WithHasher(counter))
	hash := hashOf(t, k, 1)
	before := counter.matches

	const workers = 20

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		wrongs int
		locked int
	)

	for range workers {
		wg.Go(func() {
			err := k.Reauth.Confirm(context.Background(), 1, hash, "nope")

			mu.Lock()
			defer mu.Unlock()

			if apperr.HasReason(err, account.ReasonReauthLocked) {
				locked++
			} else if err == account.ErrWrongPassword { //nolint:errorlint // the sentinel itself
				wrongs++
			}
		})
	}

	wg.Wait()

	require.Equal(t, before+5, counter.matches, "five passwords checked, whatever the parallelism")
	require.Equal(t, 4, wrongs)
	require.Equal(t, workers-4, locked)
}

func TestTheLockCanBeTuned(t *testing.T) {
	k := seeded(t, identitytest.WithReauthLock(account.Lock{After: 2, For: time.Minute}))
	hash := hashOf(t, k, 1)

	require.ErrorIs(t, k.Reauth.Confirm(t.Context(), 1, hash, "nope"), account.ErrWrongPassword)
	require.True(t, apperr.HasReason(k.Reauth.Confirm(t.Context(), 1, hash, "nope"), account.ReasonReauthLocked))

	k.Clock.Advance(time.Minute)
	require.NoError(t, k.Reauth.Confirm(t.Context(), 1, hash, "secret"))
}

func TestNewReauthNamesWhatIsMissing(t *testing.T) {
	clock := identitytest.NewClock(identitytest.Epoch)

	for want, d := range map[string]account.ReauthDeps{
		"ReauthDeps.Store":  {Hasher: identitytest.Hasher, Clock: clock},
		"ReauthDeps.Hasher": {Store: identitytest.NewReauth(), Clock: clock},
		"ReauthDeps.Clock":  {Store: identitytest.NewReauth(), Hasher: identitytest.Hasher},
	} {
		_, err := account.NewReauth(d, account.Lock{})
		require.ErrorContains(t, err, want)
	}
}
