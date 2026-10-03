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

func TestGetAndChangeLocale(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	got, err := k.Users.Get(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "ana", got.Login)

	_, err = k.Users.Get(ctx, 99)
	assert.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))

	require.NoError(t, k.Users.ChangeLocale(ctx, account.ChangeLocaleInput{AccountID: 1, Locale: "hr"}))

	got, _ = k.Users.Get(ctx, 1)
	assert.Equal(t, "hr", got.Locale)

	for _, bad := range []string{"", "HR", "croatian!", "h"} {
		err = k.Users.ChangeLocale(ctx, account.ChangeLocaleInput{AccountID: 1, Locale: bad})
		assert.True(t, apperr.HasReason(err, account.ReasonLocaleInvalid), bad)
	}

	err = k.Users.ChangeLocale(ctx, account.ChangeLocaleInput{AccountID: 99, Locale: "hr"})
	assert.True(t, apperr.HasReason(err, account.ReasonAccountNotFound))
}

// IAM-USER-004: sessions are listed latest used first, and ended one at a time.
func TestSessionsAreListedAndRevoked(t *testing.T) {
	n := &notices{}
	k := seeded(t, identitytest.WithNotices(n))
	ctx := t.Context()

	a, err := k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err)
	k.Clock.Advance(time.Minute)

	_, err = k.SignIn.Login(ctx, account.LoginInput{Login: "ana", Password: "secret", Device: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/129.0 Safari/537.36"})
	require.NoError(t, err)

	sessions, err := k.Users.Sessions(ctx, 1)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	assert.Equal(t, "Chrome · macOS", sessions[0].Label(), "the latest used first")

	current, err := k.SignIn.Authenticate(ctx, a.Credentials.Access)
	require.NoError(t, err)

	err = k.Users.RevokeSession(ctx, account.RevokeSessionInput{AccountID: 1, SessionID: current.Session.ID, Keep: current.Session.ID})
	assert.True(t, apperr.HasReason(err, account.ReasonSessionCurrent))

	err = k.Users.RevokeSession(ctx, account.RevokeSessionInput{AccountID: 2, SessionID: sessions[0].ID})
	assert.True(t, apperr.HasReason(err, account.ReasonSessionNotFound), "somebody else's session")

	require.NoError(t, k.Users.RevokeSession(ctx, account.RevokeSessionInput{AccountID: 1, SessionID: sessions[0].ID, ActorID: 2}))

	last := k.SignIns.All()[len(k.SignIns.All())-1]
	assert.Equal(t, account.SignInEntry{ID: last.ID, AccountID: 1, Event: account.SessionRevoked, ActorID: 2, CreatedAt: k.Clock.Now()}, last)
	assert.Equal(t, []string{"revoked 1 by 2 x1"}, n.events)
}

// IAM-USER-004 items 4 to 6: ending all keeps the caller's own and ends what the person opened as somebody else.
func TestRevokeSessionsKeepsTheCallersAndEndsWhatWasOpenedAsSomebodyElse(t *testing.T) {
	k := seeded(t, identitytest.WithImpersonation(permitAll{}))
	ctx := t.Context()

	here, err := k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err)

	elsewhere, err := k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err)

	asBoris, err := k.SignIn.LoginAs(ctx, account.LoginAsInput{ActorID: 1, TargetID: 2})
	require.NoError(t, err)

	require.NoError(t, k.Users.RevokeSessions(ctx, account.RevokeSessionsInput{AccountID: 1, KeepToken: here.Credentials.Access}))

	_, err = k.SignIn.Authenticate(ctx, here.Credentials.Access)
	require.NoError(t, err, "the session it was done from stays")

	_, err = k.SignIn.Authenticate(ctx, elsewhere.Credentials.Access)
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid))

	_, err = k.SignIn.Authenticate(ctx, asBoris.Credentials.Access)
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid), "a way in as somebody else does not outlive the person")

	var got []account.SignInEvent

	for _, e := range k.SignIns.All() {
		if e.AccountID == 2 {
			got = append(got, e.Event)
		}
	}

	assert.Equal(t, []account.SignInEvent{account.SignedInAs, account.SessionRevoked}, got, "on the target's history too")
}

func TestRevokeSessionsOfAnotherAccountsKeepTokenIsIgnored(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	mine, err := k.SignIn.Login(ctx, login("ana", "secret"))
	require.NoError(t, err)

	theirs, err := k.SignIn.Login(ctx, login("boris", "hunter2"))
	require.NoError(t, err)

	require.NoError(t, k.Users.RevokeSessions(ctx, account.RevokeSessionsInput{AccountID: 1, KeepToken: theirs.Credentials.Access}))

	_, err = k.SignIn.Authenticate(ctx, mine.Credentials.Access)
	assert.True(t, apperr.HasReason(err, account.ReasonSessionInvalid))

	_, err = k.SignIn.Authenticate(ctx, theirs.Credentials.Access)
	require.NoError(t, err)
}

func TestDeviceLabel(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36":                       "Chrome · macOS",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36 Edg/129.0":                   "Edge · Windows",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:130.0) Gecko/20100101 Firefox/130.0":                                                        "Firefox · Windows",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1": "Safari · iPhone",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148":                           "Safari · iPhone",
		"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/129.0 Mobile/15E148 Safari/604.1":           "Chrome · iPad",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Mobile Safari/537.36":                       "Chrome · Android",
		"Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0":                                                                  "Firefox · Linux",
		"curl/8.4.0": "curl/8.4.0",
		"  ":         "",
	}

	for ua, want := range cases {
		assert.Equal(t, want, account.DeviceLabel(ua), ua)
	}

	assert.Len(t, []rune(account.DeviceLabel(string(make([]rune, 300)))), 80)
}

func TestLockedUntilReadsTheLatestEvents(t *testing.T) {
	at := identitytest.Epoch
	wrong := func(ago time.Duration) account.SignInEntry {
		return account.SignInEntry{Event: account.WrongPassword, CreatedAt: at.Add(-ago)}
	}

	l := account.Lock{}
	latest := []account.SignInEntry{wrong(time.Minute), wrong(2 * time.Minute), wrong(3 * time.Minute), wrong(4 * time.Minute), wrong(5 * time.Minute)}

	until, locked := l.LockedUntil(latest, at)
	assert.True(t, locked)
	assert.Equal(t, at.Add(14*time.Minute), until)

	_, locked = l.LockedUntil(latest[:4], at)
	assert.False(t, locked, "four are not enough")

	_, locked = l.LockedUntil(latest, at.Add(14*time.Minute))
	assert.False(t, locked, "the lock has run out")

	latest[2].Event = account.SignedIn
	_, locked = l.LockedUntil(latest, at)
	assert.False(t, locked, "a sign-in among them")
}
