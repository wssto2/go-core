package account_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

func login(login, password string) account.LoginInput {
	return account.LoginInput{Login: login, Password: password, Device: "test", IP: "10.0.0.1"}
}

func events(k identitytest.Kit) []account.SignInEvent {
	var out []account.SignInEvent
	for _, e := range k.SignIns.All() {
		out = append(out, e.Event)
	}

	return out
}

// IAM-USER-001: an unknown login and a wrong password answer exactly alike, after the same work.
func TestUnknownLoginAndWrongPasswordAreIndistinguishable(t *testing.T) {
	counter := &countingHasher{PasswordHasher: identitytest.Hasher}
	k := seeded(t, identitytest.WithHasher(counter))
	ctx := t.Context()

	_, unknown := k.SignIn.Login(ctx, login("nobody", "whatever"))
	_, wrong := k.SignIn.Login(ctx, login("ana", "whatever"))

	require.Error(t, unknown)
	require.Error(t, wrong)

	var a, b *apperr.AppError
	require.ErrorAs(t, unknown, &a)
	require.ErrorAs(t, wrong, &b)

	assert.Equal(t, account.ReasonSignInFailed, a.Reason)
	assert.Equal(t, a.Reason, b.Reason)
	assert.Equal(t, a.Code, b.Code)
	assert.Equal(t, a.Message, b.Message)
	assert.Equal(t, a.Params, b.Params)
	assert.Equal(t, 2, counter.matches, "a password is compared for the unknown login too")

	assert.Equal(t, []account.SignInEvent{account.WrongPassword}, events(k), "an unknown login is not recorded")
}

// IAM-USER-001 item 3: inactive is said only to whoever gave the right password.
func TestInactiveIsSaidOnlyForTheRightPassword(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	_, err := k.SignIn.Login(ctx, login("ines", "wrong"))
	assert.True(t, apperr.HasReason(err, account.ReasonSignInFailed))

	_, err = k.SignIn.Login(ctx, login("ines", "secret"))
	assert.True(t, apperr.HasReason(err, account.ReasonSignInInactive))
	assert.Equal(t, []account.SignInEvent{account.WrongPassword, account.RefusedInactive}, events(k))
}

func TestLoginSignsInAndRecordsIt(t *testing.T) {
	k := seeded(t)

	signed, err := k.SignIn.Login(t.Context(), login(" Ana ", "secret"))
	require.NoError(t, err)

	assert.Equal(t, 1, signed.Account.ID)
	assert.NotEmpty(t, signed.Credentials.Access)
	assert.NotEmpty(t, signed.Credentials.Refresh)
	assert.Equal(t, identitytest.Epoch.Add(24*time.Hour), signed.Credentials.ExpiresAt)
	assert.Equal(t, []account.SignInEvent{account.SignedIn}, events(k))

	got, err := k.SignIn.Authenticate(t.Context(), signed.Credentials.Access)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Account.ID)
}

// IAM-USER-002: five wrong passwords lock fifteen minutes; the lock is read off the history.
func TestFiveWrongPasswordsLockForFifteenMinutes(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	for range 5 {
		_, err := k.SignIn.Login(ctx, login("ana", "nope"))
		assert.True(t, apperr.HasReason(err, account.ReasonSignInFailed))
		k.Clock.Advance(time.Second)
	}

	// Locked: even the right password is refused, and is not compared (item 4).
	_, err := k.SignIn.Login(ctx, login("ana", "secret"))
	require.Error(t, err)

	var locked *apperr.AppError
	require.ErrorAs(t, err, &locked)
	assert.Equal(t, account.ReasonSignInLocked, locked.Reason)
	assert.Equal(t, identitytest.Epoch.Add(4*time.Second).Add(15*time.Minute).Format(time.RFC3339), locked.Params["locked_until"])
	assert.Equal(t, account.LockedOut, k.SignIns.All()[5].Event)

	// A refusal while locked does not extend the lock (item 2).
	k.Clock.Advance(14 * time.Minute)
	_, err = k.SignIn.Login(ctx, login("ana", "secret"))
	assert.True(t, apperr.HasReason(err, account.ReasonSignInLocked))

	k.Clock.Advance(time.Minute)

	_, err = k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err, "the lock has run out")
}

// IAM-USER-002 item 3: after a lock has run out, the next wrong password locks again.
func TestAWrongPasswordAfterTheLockLocksAgain(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	for range 5 {
		_, _ = k.SignIn.Login(ctx, login("ana", "nope"))
	}

	k.Clock.Advance(16 * time.Minute)

	_, err := k.SignIn.Login(ctx, login("ana", "nope"))
	assert.True(t, apperr.HasReason(err, account.ReasonSignInFailed), "the password is checked again")

	_, err = k.SignIn.Login(ctx, login("ana", "secret"))
	assert.True(t, apperr.HasReason(err, account.ReasonSignInLocked), "and the latest five are all wrong")
}

// IAM-USER-002 item 2: a sign-in or an unlock starts the count again.
func TestSignInOrUnlockStartsTheCountAgain(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	for range 4 {
		_, _ = k.SignIn.Login(ctx, login("ana", "nope"))
	}

	_, err := k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err)

	for range 4 {
		_, _ = k.SignIn.Login(ctx, login("ana", "nope"))
	}

	_, err = k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err, "four wrong passwords after a sign-in do not lock")

	for range 5 {
		_, _ = k.SignIn.Login(ctx, login("boris", "nope"))
	}

	require.NoError(t, k.SignIns.Record(ctx, account.SignInEntry{AccountID: 2, Event: account.Unlocked, ActorID: 1, CreatedAt: k.Clock.Now()}))

	_, err = k.SignIn.Login(ctx, login("boris", "hunter2"))
	require.NoError(t, err, "an unlock lifts the lock")
}

func TestLockIsConfigurable(t *testing.T) {
	k := seeded(t, identitytest.WithConfig(account.Config{Lock: account.Lock{After: 2, For: time.Minute}}))
	ctx := t.Context()

	_, _ = k.SignIn.Login(ctx, login("ana", "nope"))
	_, _ = k.SignIn.Login(ctx, login("ana", "nope"))

	_, err := k.SignIn.Login(ctx, login("ana", "secret"))
	assert.True(t, apperr.HasReason(err, account.ReasonSignInLocked))

	k.Clock.Advance(time.Minute)

	_, err = k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err)
}

// IAM-USER-001 item 5: at most ten attempts per login per minute, whatever the answers.
func TestAttemptsPerMinuteAreLimited(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	for range 10 {
		_, err := k.SignIn.Login(ctx, login("nobody", "x"))
		assert.True(t, apperr.HasReason(err, account.ReasonSignInFailed))
	}

	_, err := k.SignIn.Login(ctx, login(" NOBODY ", "x"))

	var refused *apperr.AppError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, account.ReasonSignInLocked, refused.Reason)
	assert.Equal(t, identitytest.Epoch.Add(time.Minute).Format(time.RFC3339), refused.Params["locked_until"])

	k.Clock.Advance(time.Minute)

	_, err = k.SignIn.Login(ctx, login("nobody", "x"))
	assert.True(t, apperr.HasReason(err, account.ReasonSignInFailed), "the next window starts")
}

func TestRefreshRotatesTheTokens(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	first, err := k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err)

	k.Clock.Advance(time.Hour)

	second, err := k.SignIn.Refresh(ctx, account.RefreshInput{Token: first.Credentials.Refresh, Device: "new device"})
	require.NoError(t, err)
	assert.NotEqual(t, first.Credentials.Access, second.Credentials.Access)
	assert.Equal(t, k.Clock.Now().Add(24*time.Hour), second.Credentials.ExpiresAt)

	_, err = k.SignIn.Authenticate(ctx, first.Credentials.Access)
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid), "the old access token stops working")

	_, err = k.SignIn.Refresh(ctx, account.RefreshInput{Token: first.Credentials.Refresh})
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid), "a refresh token works once")

	_, err = k.SignIn.Authenticate(ctx, second.Credentials.Access)
	require.NoError(t, err)

	sessions, err := k.Users.Sessions(ctx, 1)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "new device", sessions[0].Device)
}

func TestRefreshOfADeactivatedAccountIsRefused(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	signed, err := k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err)
	require.NoError(t, k.Accounts.SetActive(ctx, 1, false))

	_, err = k.SignIn.Refresh(ctx, account.RefreshInput{Token: signed.Credentials.Refresh})
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid))

	_, err = k.SignIn.Authenticate(ctx, signed.Credentials.Access)
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid))
}

func TestAuthenticateRefusesAnExpiredToken(t *testing.T) {
	k := seeded(t)

	signed, err := k.SignIn.Login(t.Context(), login("ana", "secret"))
	require.NoError(t, err)

	k.Clock.Advance(24 * time.Hour)

	_, err = k.SignIn.Authenticate(t.Context(), signed.Credentials.Access)
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid))

	_, err = k.SignIn.Authenticate(t.Context(), "")
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid))
}

func TestAuthenticateTouchesTheSessionAtMostOncePerMinute(t *testing.T) {
	k := seeded(t)

	signed, err := k.SignIn.Login(t.Context(), login("ana", "secret"))
	require.NoError(t, err)

	k.Clock.Advance(30 * time.Second)

	got, err := k.SignIn.Authenticate(t.Context(), signed.Credentials.Access)
	require.NoError(t, err)
	assert.Equal(t, identitytest.Epoch, got.Session.LastUsedAt)

	k.Clock.Advance(31 * time.Second)

	_, err = k.SignIn.Authenticate(t.Context(), signed.Credentials.Access)
	require.NoError(t, err)

	sessions, err := k.Users.Sessions(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, k.Clock.Now(), sessions[0].LastUsedAt)
}

func TestLogoutEndsTheSession(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	signed, err := k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err)

	require.NoError(t, k.SignIn.Logout(ctx, signed.Credentials.Access))

	_, err = k.SignIn.Authenticate(ctx, signed.Credentials.Access)
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid))
	assert.True(t, apperr.HasReason(k.SignIn.Logout(ctx, signed.Credentials.Access), account.ReasonSessionInvalid))
}

func TestLoginAsIsOffUnlessTheApplicationSaysWhoMay(t *testing.T) {
	k := seeded(t)

	_, err := k.SignIn.LoginAs(t.Context(), account.LoginAsInput{ActorID: 1, TargetID: 2})
	assert.True(t, apperr.HasReason(err, account.ReasonImpersonationDisabled))
}

func TestLoginAs(t *testing.T) {
	n := &notices{}
	k := seeded(t, identitytest.WithImpersonation(permitAll{refuse: map[int]bool{4: true}}), identitytest.WithNotices(n))
	ctx := t.Context()

	_, err := k.Accounts.Create(ctx, identitytest.Account(4, "dora", "x"))
	require.NoError(t, err)

	_, err = k.SignIn.LoginAs(ctx, account.LoginAsInput{ActorID: 1, TargetID: 99})
	assert.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))

	_, err = k.SignIn.LoginAs(ctx, account.LoginAsInput{ActorID: 1, TargetID: 3})
	assert.True(t, apperr.HasReason(err, account.ReasonAccountInactive))

	_, err = k.SignIn.LoginAs(ctx, account.LoginAsInput{ActorID: 1, TargetID: 4})
	require.Error(t, err, "the Impersonation refuses this target")

	_, err = k.SignIn.LoginAs(ctx, account.LoginAsInput{ActorID: 3, TargetID: 2})
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid), "an inactive actor is not signed in")

	signed, err := k.SignIn.LoginAs(ctx, account.LoginAsInput{ActorID: 1, TargetID: 2, Device: "ana's laptop"})
	require.NoError(t, err)
	assert.Equal(t, 2, signed.Account.ID)

	got, err := k.SignIn.Authenticate(ctx, signed.Credentials.Access)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Session.ActorID, "the session says who signed in")
	require.NotNil(t, got.Actor)
	assert.Equal(t, 1, got.Actor.ID)
	assert.Equal(t, 1, signed.Actor.ID)

	all := k.SignIns.All()
	require.Len(t, all, 1)
	assert.Equal(t, account.SignInEntry{
		ID: 1, AccountID: 2, Event: account.SignedInAs, ActorID: 1, Device: "ana's laptop", CreatedAt: identitytest.Epoch,
	}, all[0], "the target's history says who")

	require.NoError(t, k.SignIn.Logout(ctx, signed.Credentials.Access))
	assert.Equal(t, []string{"in 1>2", "out 1>2"}, n.events)
}

func TestRefreshKeepsALoginAsSessionMarked(t *testing.T) {
	k := seeded(t, identitytest.WithImpersonation(permitAll{}))

	signed, err := k.SignIn.LoginAs(t.Context(), account.LoginAsInput{ActorID: 1, TargetID: 2})
	require.NoError(t, err)

	refreshed, err := k.SignIn.Refresh(t.Context(), account.RefreshInput{Token: signed.Credentials.Refresh, Device: "other"})
	require.NoError(t, err)

	got, err := k.SignIn.Authenticate(t.Context(), refreshed.Credentials.Access)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Session.ActorID)
	assert.Equal(t, 1, refreshed.Actor.ID)
}

func TestReturnGivesBackTheActorsOwnSession(t *testing.T) {
	n := &notices{}
	k := seeded(t, identitytest.WithImpersonation(permitAll{}), identitytest.WithNotices(n))
	ctx := t.Context()

	own, err := k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret"})
	require.NoError(t, err)

	got, err := k.SignIn.Authenticate(ctx, own.Credentials.Access)
	require.NoError(t, err)

	_, err = k.SignIn.Return(ctx, account.ReturnInput{Session: got.Session})
	assert.True(t, apperr.HasReason(err, account.ReasonImpersonationNotActive), "a person's own session has nothing to return from")

	as, err := k.SignIn.LoginAs(ctx, account.LoginAsInput{ActorID: 1, TargetID: 2})
	require.NoError(t, err)

	asSession, err := k.SignIn.Authenticate(ctx, as.Credentials.Access)
	require.NoError(t, err)

	back, err := k.SignIn.Return(ctx, account.ReturnInput{Session: asSession.Session, Device: "ana's laptop"})
	require.NoError(t, err)
	assert.Equal(t, 1, back.Account.ID)
	assert.Nil(t, back.Actor)

	_, err = k.SignIn.Authenticate(ctx, as.Credentials.Access)
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid), "the impersonation session is gone")

	again, err := k.SignIn.Authenticate(ctx, back.Credentials.Access)
	require.NoError(t, err)
	assert.Zero(t, again.Session.ActorID)
	assert.Nil(t, again.Actor)
	assert.Equal(t, []string{"in 1>2", "out 1>2"}, n.events)
}

func TestNewNamesTheMissingDependency(t *testing.T) {
	_, err := account.New(account.Deps{}, account.Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Deps.Accounts is missing")
}
