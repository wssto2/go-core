// Package storetest is the conformance suite every identity store must pass:
// the account, sign-in history, session, code and re-confirmation stores of
// identity/account.
//
//	func TestMyStores(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) storetest.Stores { return storetest.Stores{...} })
//	}
//
// Each subtest gets fresh, empty stores from the factory, so the factory's
// database is empty for every call. The stores must be safe for concurrent
// use: the refresh-rotation test calls one from several goroutines.
package storetest

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wssto2/go-core/identity/account"
)

// Stores are the stores under test, over one database.
type Stores struct {
	Accounts account.Store
	SignIns  account.SignInLog
	Sessions account.SessionStore
	Codes    account.CodeStore
	Reauth   account.ReauthStore
}

// Factory returns new empty stores for one subtest.
type Factory func(t *testing.T) Stores

// base is a time to the whole second, which every database keeps.
var base = time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)

type check struct{ t *testing.T }

func (c check) noErr(err error, what string) {
	c.t.Helper()

	if err != nil {
		c.t.Fatalf("%s: unexpected error: %v", what, err)
	}
}

func (c check) isErr(err, target error, what string) {
	c.t.Helper()

	if !errors.Is(err, target) {
		c.t.Errorf("%s: got error %v, want %v", what, err, target)
	}
}

func (c check) equal(want, got any, what string) {
	c.t.Helper()

	if !reflect.DeepEqual(want, got) {
		c.t.Errorf("%s: got %#v, want %#v", what, got, want)
	}
}

func (c check) true(ok bool, what string) {
	c.t.Helper()

	if !ok {
		c.t.Errorf("%s", what)
	}
}

// same compares times by the instant, not the zone.
func (c check) same(want, got time.Time, what string) {
	c.t.Helper()

	if !want.Equal(got) {
		c.t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

// Run runs every conformance test against stores from newStores.
func Run(t *testing.T, newStores Factory) {
	t.Helper()

	for name, fn := range map[string]func(*testing.T, Stores){
		"accounts/create and find":       accountsCreateAndFind,
		"accounts/login is unique":       accountsLoginUnique,
		"accounts/missing":               accountsMissing,
		"accounts/find many":             accountsFindMany,
		"accounts/setters":               accountsSetters,
		"accounts/inactive stays so":     accountsInactive,
		"signins/lock events":            signInsLockEvents,
		"signins/round trip":             signInsRoundTrip,
		"sessions/open and get":          sessionsOpenAndGet,
		"sessions/get refuses":           sessionsGetRefuses,
		"sessions/touch":                 sessionsTouch,
		"sessions/rotate":                sessionsRotate,
		"sessions/rotate refuses":        sessionsRotateRefuses,
		"sessions/rotate once":           sessionsRotateOnce,
		"sessions/live":                  sessionsLive,
		"sessions/end":                   sessionsEnd,
		"sessions/end opened by":         sessionsEndOpenedBy,
		"sessions/long device name":      sessionsLongDevice,
		"sessions/tokens are not stored": sessionsTokensDiffer,
		"codes/issue and latest":         codesIssueAndLatest,
		"codes/issue ends the live one":  codesIssueEndsLive,
		"codes/issued since":             codesIssuedSince,
		"codes/save verification":        codesSaveVerification,
		"codes/consumed once":            codesConsumedOnce,
		"codes/attempts are counted":     codesAttemptsCounted,
		"codes/invalidate live":          codesInvalidateLive,
		"reauth/find, create and save":   reauthFindCreateSave,
		"reauth/one counter wins":        reauthOneWins,
	} {
		t.Run(name, func(t *testing.T) { fn(t, newStores(t)) })
	}
}

func ctx() context.Context { return context.Background() }

func newAccount(login string) account.Account {
	return account.Account{
		Login: login, Email: strings.ToLower(login) + "@example.test", Name: strings.ToUpper(login[:1]) + login[1:], Locale: "hr",
		Active: true, PasswordHash: "hash-" + login, CreatedAt: base,
	}
}

func accountsCreateAndFind(t *testing.T, s Stores) {
	c := check{t}

	created, err := s.Accounts.Create(ctx(), newAccount("Ana"))
	c.noErr(err, "create")
	c.true(created.ID > 0, "create assigns an id")
	c.equal("ana", created.Login, "the login is stored normalised")

	byID, err := s.Accounts.Find(ctx(), created.ID)
	c.noErr(err, "find")
	c.equal(created.ID, byID.ID, "id")
	c.equal("ana", byID.Login, "login")
	c.equal("ana@example.test", byID.Email, "email")
	c.equal("Ana", byID.Name, "name")
	c.equal("hr", byID.Locale, "locale")
	c.equal("hash-Ana", byID.PasswordHash, "password hash")
	c.true(byID.Active, "active")
	c.same(base, byID.CreatedAt, "created at")

	byLogin, err := s.Accounts.FindByLogin(ctx(), " ANA ")
	c.noErr(err, "find by login")
	c.equal(byID.ID, byLogin.ID, "find by login ignores case and space")

	second, err := s.Accounts.Create(ctx(), newAccount("boris"))
	c.noErr(err, "second create")
	c.true(second.ID != created.ID, "ids differ")
}

func accountsLoginUnique(t *testing.T, s Stores) {
	c := check{t}

	_, err := s.Accounts.Create(ctx(), newAccount("ana"))
	c.noErr(err, "create")

	_, err = s.Accounts.Create(ctx(), newAccount("ana"))
	c.isErr(err, account.ErrLoginTaken, "same login")

	_, err = s.Accounts.Create(ctx(), newAccount("ANA"))
	c.isErr(err, account.ErrLoginTaken, "same login in another case")
}

func accountsMissing(t *testing.T, s Stores) {
	c := check{t}

	_, err := s.Accounts.Find(ctx(), 999)
	c.isErr(err, account.ErrNotFound, "find")

	_, err = s.Accounts.FindByLogin(ctx(), "nobody")
	c.isErr(err, account.ErrNotFound, "find by login")
}

func accountsFindMany(t *testing.T, s Stores) {
	c := check{t}

	a, _ := s.Accounts.Create(ctx(), newAccount("ana"))
	b, _ := s.Accounts.Create(ctx(), newAccount("boris"))

	got, err := s.Accounts.FindMany(ctx(), []int{a.ID, b.ID, 999})
	c.noErr(err, "find many")
	c.equal(2, len(got), "the ones that exist")

	none, err := s.Accounts.FindMany(ctx(), nil)
	c.noErr(err, "find none")
	c.equal(0, len(none), "nothing asked, nothing found")
}

func accountsSetters(t *testing.T, s Stores) {
	c := check{t}

	a, err := s.Accounts.Create(ctx(), newAccount("ana"))
	c.noErr(err, "create")

	c.noErr(s.Accounts.SetLocale(ctx(), a.ID, "en"), "set locale")
	c.noErr(s.Accounts.SetLocale(ctx(), a.ID, "en"), "set locale to what it is: not a missing account")
	c.noErr(s.Accounts.SetPasswordHash(ctx(), a.ID, "new-hash"), "set hash")
	c.noErr(s.Accounts.SetActive(ctx(), a.ID, false), "deactivate")

	got, err := s.Accounts.Find(ctx(), a.ID)
	c.noErr(err, "find")
	c.equal("en", got.Locale, "locale")
	c.equal("new-hash", got.PasswordHash, "hash")
	c.true(!got.Active, "inactive")

	c.noErr(s.Accounts.SetActive(ctx(), a.ID, true), "activate")

	got, _ = s.Accounts.Find(ctx(), a.ID)
	c.true(got.Active, "active again")

	c.isErr(s.Accounts.SetLocale(ctx(), 999, "en"), account.ErrNotFound, "locale of nobody")
	c.isErr(s.Accounts.SetPasswordHash(ctx(), 999, "x"), account.ErrNotFound, "hash of nobody")
	c.isErr(s.Accounts.SetActive(ctx(), 999, true), account.ErrNotFound, "active of nobody")
}

func accountsInactive(t *testing.T, s Stores) {
	c := check{t}

	a := newAccount("ines")
	a.Active = false

	created, err := s.Accounts.Create(ctx(), a)
	c.noErr(err, "create")

	got, err := s.Accounts.Find(ctx(), created.ID)
	c.noErr(err, "find")
	c.true(!got.Active, "an account created inactive reads inactive")
}

func entry(accountID int, e account.Event, at time.Duration) account.SignInEntry {
	return account.SignInEntry{AccountID: accountID, Event: e, CreatedAt: base.Add(at), IP: "10.0.0.1", Device: "test"}
}

func signInsLockEvents(t *testing.T, s Stores) {
	c := check{t}

	for _, e := range []account.SignInEntry{
		entry(1, account.WrongPassword, 1*time.Second),
		entry(1, account.LockedOut, 2*time.Second), // not a lock event
		entry(2, account.WrongPassword, 3*time.Second),
		entry(1, account.SignedIn, 4*time.Second),
		entry(1, account.RefusedInactive, 5*time.Second), // not a lock event
		entry(1, account.WrongPassword, 6*time.Second),
		entry(1, account.Unlocked, 7*time.Second),
	} {
		c.noErr(s.SignIns.Record(ctx(), e), "record")
	}

	got, err := s.SignIns.LockEvents(ctx(), 1, 3)
	c.noErr(err, "lock events")

	var events []account.Event
	for _, e := range got {
		events = append(events, e.Event)
	}

	c.equal([]account.Event{account.Unlocked, account.WrongPassword, account.SignedIn}, events, "the latest three lock events, newest first")

	all, err := s.SignIns.LockEvents(ctx(), 1, 10)
	c.noErr(err, "lock events")
	c.equal(4, len(all), "every lock event of the account, and only its own")

	none, err := s.SignIns.LockEvents(ctx(), 3, 5)
	c.noErr(err, "lock events")
	c.equal(0, len(none), "an account without any")
}

func signInsRoundTrip(t *testing.T, s Stores) {
	c := check{t}

	in := account.SignInEntry{
		AccountID: 7, Event: account.Unlocked, ActorID: 9, CreatedAt: base,
		IP: strings.Repeat("1", account.IPMax), Device: strings.Repeat("d", account.DeviceMax),
	}
	c.noErr(s.SignIns.Record(ctx(), in), "record")
	c.noErr(s.SignIns.Record(ctx(), account.SignInEntry{AccountID: 7, Event: account.WrongPassword, CreatedAt: base.Add(time.Second)}), "record without an actor")

	got, err := s.SignIns.LockEvents(ctx(), 7, 5)
	c.noErr(err, "lock events")
	c.equal(2, len(got), "rows")

	c.equal(account.WrongPassword, got[0].Event, "newest first")
	c.equal(0, got[0].ActorID, "no actor reads zero")
	c.same(base.Add(time.Second), got[0].CreatedAt, "created at")

	c.equal(9, got[1].ActorID, "actor")
	c.equal(in.IP, got[1].IP, "ip at its full width")
	c.equal(in.Device, got[1].Device, "device at its full width")
	c.equal(7, got[1].AccountID, "account")
	c.true(got[1].ID > 0 && got[1].ID < got[0].ID, "ids ascend with time")
}

func open(c check, s Stores, accountID, actorID int, device string, at time.Time) account.Credentials {
	c.t.Helper()

	creds, err := s.Sessions.Open(ctx(), account.NewSession{
		AccountID: accountID, ActorID: actorID, Device: device, IP: "10.0.0.1", At: at, ExpiresAt: at.Add(24 * time.Hour),
	})
	c.noErr(err, "open")
	c.true(creds.Access != "" && creds.Refresh != "" && creds.Access != creds.Refresh, "tokens are issued")
	c.same(at.Add(24*time.Hour), creds.ExpiresAt, "credentials expiry")

	return creds
}

func sessionsOpenAndGet(t *testing.T, s Stores) {
	c := check{t}

	creds := open(c, s, 1, 0, "Firefox", base)
	asOther := open(c, s, 2, 1, "Chrome", base)

	got, err := s.Sessions.Get(ctx(), creds.Access, base.Add(time.Hour))
	c.noErr(err, "get")
	c.true(got.ID > 0, "id")
	c.equal(1, got.AccountID, "account")
	c.equal(0, got.ActorID, "no actor")
	c.equal("Firefox", got.Device, "device")
	c.equal("10.0.0.1", got.IP, "ip")
	c.same(base, got.CreatedAt, "created at")
	c.same(base, got.LastUsedAt, "last used at")
	c.same(base.Add(24*time.Hour), got.ExpiresAt, "expires at")

	got, err = s.Sessions.Get(ctx(), asOther.Access, base)
	c.noErr(err, "get a session opened as somebody else")
	c.equal(2, got.AccountID, "its account is the target")
	c.equal(1, got.ActorID, "the actor")
	c.equal("Chrome", got.Device, "device without the marker")
}

func sessionsGetRefuses(t *testing.T, s Stores) {
	c := check{t}

	creds := open(c, s, 1, 0, "Firefox", base)

	_, err := s.Sessions.Get(ctx(), "unknown", base)
	c.isErr(err, account.ErrSessionNotFound, "unknown token")

	_, err = s.Sessions.Get(ctx(), creds.Refresh, base)
	c.isErr(err, account.ErrSessionNotFound, "the refresh token is not an access token")

	_, err = s.Sessions.Get(ctx(), creds.Access, base.Add(24*time.Hour))
	c.isErr(err, account.ErrSessionNotFound, "expired")

	got, err := s.Sessions.Get(ctx(), creds.Access, base)
	c.noErr(err, "get")

	n, err := s.Sessions.End(ctx(), 1, []int{got.ID}, 0, base)
	c.noErr(err, "end")
	c.equal(1, n, "ended")

	_, err = s.Sessions.Get(ctx(), creds.Access, base)
	c.isErr(err, account.ErrSessionNotFound, "revoked")
}

func sessionsTouch(t *testing.T, s Stores) {
	c := check{t}

	creds := open(c, s, 1, 0, "Firefox", base)
	got, _ := s.Sessions.Get(ctx(), creds.Access, base)

	c.noErr(s.Sessions.Touch(ctx(), got.ID, base.Add(time.Hour), "10.0.0.9"), "touch")

	got, err := s.Sessions.Get(ctx(), creds.Access, base)
	c.noErr(err, "get")
	c.same(base.Add(time.Hour), got.LastUsedAt, "last used at")
	c.equal("10.0.0.9", got.IP, "ip")
}

func sessionsRotate(t *testing.T, s Stores) {
	c := check{t}

	plain := open(c, s, 1, 0, "Firefox", base)
	asOther := open(c, s, 2, 7, "Chrome", base)
	later := base.Add(time.Hour)

	session, creds, err := s.Sessions.Rotate(ctx(), account.Rotation{
		Refresh: plain.Refresh, At: later, ExpiresAt: later.Add(24 * time.Hour), Device: "Safari", IP: "10.0.0.2",
	})
	c.noErr(err, "rotate")
	c.equal(1, session.AccountID, "the session's account")
	c.equal("Safari", session.Device, "the device is replaced")
	c.true(creds.Access != plain.Access && creds.Refresh != plain.Refresh, "new tokens")
	c.same(later.Add(24*time.Hour), creds.ExpiresAt, "new expiry")

	got, err := s.Sessions.Get(ctx(), creds.Access, later)
	c.noErr(err, "the new access token works")
	c.equal(session.ID, got.ID, "same session")
	c.equal("Safari", got.Device, "device")
	c.equal("10.0.0.2", got.IP, "ip")
	c.same(later, got.LastUsedAt, "last used")
	c.same(later.Add(24*time.Hour), got.ExpiresAt, "expiry")

	_, err = s.Sessions.Get(ctx(), plain.Access, later)
	c.isErr(err, account.ErrSessionNotFound, "the old access token is dead")

	_, _, err = s.Sessions.Rotate(ctx(), account.Rotation{Refresh: plain.Refresh, At: later, ExpiresAt: later.Add(time.Hour)})
	c.isErr(err, account.ErrSessionNotFound, "the old refresh token is dead")

	// A rotation without a device keeps it; one with a device keeps the actor.
	again, _, err := s.Sessions.Rotate(ctx(), account.Rotation{Refresh: creds.Refresh, At: later, ExpiresAt: later.Add(time.Hour)})
	c.noErr(err, "rotate without a device")
	c.equal("Safari", again.Device, "device kept")

	opened, _, err := s.Sessions.Rotate(ctx(), account.Rotation{Refresh: asOther.Refresh, At: later, ExpiresAt: later.Add(time.Hour), Device: "Edge"})
	c.noErr(err, "rotate a session opened as somebody else")
	c.equal(7, opened.ActorID, "the actor stays")
	c.equal("Edge", opened.Device, "the new device")
	c.equal(2, opened.AccountID, "the target stays")
}

func sessionsRotateRefuses(t *testing.T, s Stores) {
	c := check{t}

	creds := open(c, s, 1, 0, "Firefox", base)
	rot := func(refresh string, at time.Time) error {
		_, _, err := s.Sessions.Rotate(ctx(), account.Rotation{Refresh: refresh, At: at, ExpiresAt: at.Add(time.Hour)})

		return err
	}

	c.isErr(rot("", base), account.ErrSessionNotFound, "empty")
	c.isErr(rot("short", base), account.ErrSessionNotFound, "too short")
	c.isErr(rot(creds.Refresh[:8]+"-not-the-token", base), account.ErrSessionNotFound, "right prefix, wrong token")
	c.isErr(rot(creds.Access, base), account.ErrSessionNotFound, "the access token is not a refresh token")
	c.isErr(rot(creds.Refresh, base.Add(24*time.Hour)), account.ErrSessionNotFound, "expired")

	got, _ := s.Sessions.Get(ctx(), creds.Access, base)
	_, err := s.Sessions.End(ctx(), 1, []int{got.ID}, 0, base)
	c.noErr(err, "end")
	c.isErr(rot(creds.Refresh, base), account.ErrSessionNotFound, "revoked")
}

// Of several requests with one refresh token, exactly one wins.
func sessionsRotateOnce(t *testing.T, s Stores) {
	c := check{t}

	creds := open(c, s, 1, 0, "Firefox", base)

	const workers = 8

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
		errs []error
	)

	for range workers {
		wg.Go(func() {
			_, _, err := s.Sessions.Rotate(ctx(), account.Rotation{Refresh: creds.Refresh, At: base, ExpiresAt: base.Add(time.Hour)})

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				wins++
			} else {
				errs = append(errs, err)
			}
		})
	}

	wg.Wait()

	c.equal(1, wins, "exactly one rotation succeeds")

	for _, err := range errs {
		c.isErr(err, account.ErrSessionNotFound, "the others find no session")
	}
}

func sessionsLive(t *testing.T, s Stores) {
	c := check{t}

	old := open(c, s, 1, 0, "old", base)
	open(c, s, 1, 0, "newer", base.Add(time.Hour))
	open(c, s, 2, 0, "somebody else", base)

	open(c, s, 1, 0, "expired", base.Add(-48*time.Hour))

	now := base.Add(2 * time.Hour)

	live, err := s.Sessions.Live(ctx(), 1, now)
	c.noErr(err, "live")
	c.equal(2, len(live), "only this account's live sessions")
	c.equal("newer", live[0].Device, "the latest used first")
	c.equal("old", live[1].Device, "then the older")

	got, _ := s.Sessions.Get(ctx(), old.Access, now)
	_, err = s.Sessions.End(ctx(), 1, []int{got.ID}, 0, now)
	c.noErr(err, "end")

	live, _ = s.Sessions.Live(ctx(), 1, now)
	c.equal(1, len(live), "a revoked session is not live")
}

func sessionsEnd(t *testing.T, s Stores) {
	c := check{t}

	a := open(c, s, 1, 0, "a", base)
	open(c, s, 1, 0, "b", base)
	open(c, s, 1, 0, "c", base)
	other := open(c, s, 2, 0, "other", base)

	live, _ := s.Sessions.Live(ctx(), 1, base)
	c.equal(3, len(live), "three live")

	first, _ := s.Sessions.Get(ctx(), a.Access, base)

	n, err := s.Sessions.End(ctx(), 1, []int{first.ID}, 0, base)
	c.noErr(err, "end one")
	c.equal(1, n, "one ended")

	n, err = s.Sessions.End(ctx(), 1, []int{first.ID}, 0, base)
	c.noErr(err, "end it again")
	c.equal(0, n, "an ended session is not ended twice")

	otherSession, _ := s.Sessions.Get(ctx(), other.Access, base)

	n, err = s.Sessions.End(ctx(), 1, []int{otherSession.ID}, 0, base)
	c.noErr(err, "end somebody else's")
	c.equal(0, n, "not the account's session")

	live, _ = s.Sessions.Live(ctx(), 1, base)
	c.equal(2, len(live), "two left")

	n, err = s.Sessions.End(ctx(), 1, nil, live[0].ID, base)
	c.noErr(err, "end all but one")
	c.equal(1, n, "everything but the kept one")

	left, _ := s.Sessions.Live(ctx(), 1, base)
	c.equal(1, len(left), "one left")
	c.equal(live[0].ID, left[0].ID, "the kept one")

	n, err = s.Sessions.End(ctx(), 1, nil, 0, base)
	c.noErr(err, "end all")
	c.equal(1, n, "the rest")

	still, _ := s.Sessions.Live(ctx(), 2, base)
	c.equal(1, len(still), "another account's session is untouched")
}

func sessionsEndOpenedBy(t *testing.T, s Stores) {
	c := check{t}

	own := open(c, s, 1, 0, "own", base)
	asBoris := open(c, s, 2, 1, "as boris", base)
	asCora := open(c, s, 3, 1, "", base)
	byOther := open(c, s, 2, 11, "by 11", base) // 11 shares the digit prefix of 1
	open(c, s, 4, 1, "expired", base.Add(-48*time.Hour))

	ended, err := s.Sessions.EndOpenedBy(ctx(), 1, base)
	c.noErr(err, "end opened by")
	c.equal(2, len(ended), "the live sessions actor 1 opened")

	accounts := map[int]bool{}
	for _, e := range ended {
		accounts[e.AccountID] = true
		c.equal(1, e.ActorID, "actor")
	}

	c.true(accounts[2] && accounts[3], "they belong to the people signed in as")

	for name, creds := range map[string]account.Credentials{"as boris": asBoris, "as cora": asCora} {
		_, err := s.Sessions.Get(ctx(), creds.Access, base)
		c.isErr(err, account.ErrSessionNotFound, name+" is ended")
	}

	for name, creds := range map[string]account.Credentials{"own": own, "by 11": byOther} {
		_, err := s.Sessions.Get(ctx(), creds.Access, base)
		c.noErr(err, name+" is untouched")
	}

	again, err := s.Sessions.EndOpenedBy(ctx(), 1, base)
	c.noErr(err, "end opened by again")
	c.equal(0, len(again), "nothing left to end")
}

func sessionsLongDevice(t *testing.T, s Stores) {
	c := check{t}

	device := strings.Repeat("x", account.DeviceMax)
	creds := open(c, s, 1, 0, device, base)

	got, err := s.Sessions.Get(ctx(), creds.Access, base)
	c.noErr(err, "get")
	c.equal(device, got.Device, "a device at the full width")

	// Signing in as somebody has to fit its marker into the same column.
	creds = open(c, s, 2, 12345, device, base)

	got, err = s.Sessions.Get(ctx(), creds.Access, base)
	c.noErr(err, "get a session opened as somebody else")
	c.equal(12345, got.ActorID, "actor")
	c.true(strings.HasPrefix(device, got.Device) && len(got.Device) >= 200, "the device is kept, at most cut")
}

func sessionsTokensDiffer(t *testing.T, s Stores) {
	c := check{t}

	seen := map[string]bool{}

	for range 5 {
		creds := open(c, s, 1, 0, "d", base)
		c.true(!seen[creds.Access] && !seen[creds.Refresh], "every token is new")

		seen[creds.Access], seen[creds.Refresh] = true, true
	}
}

func newCode(accountID int, p account.Purpose, at time.Duration) account.Code {
	return account.Code{
		AccountID: accountID, Purpose: p, Target: "new@example.test", Hash: strings.Repeat("a", 64),
		ExpiresAt: base.Add(at + 15*time.Minute), IP: "10.0.0.1", CreatedAt: base.Add(at),
	}
}

func codesIssueAndLatest(t *testing.T, s Stores) {
	c := check{t}

	_, err := s.Codes.Latest(ctx(), 1, account.PurposeEmailChange)
	c.isErr(err, account.ErrCodeNotFound, "no code yet")

	issued, err := s.Codes.Issue(ctx(), newCode(1, account.PurposeEmailChange, 0), base)
	c.noErr(err, "issue")
	c.true(issued.ID > 0, "the code has an id")

	got, err := s.Codes.Latest(ctx(), 1, account.PurposeEmailChange)
	c.noErr(err, "latest")
	c.equal(issued.ID, got.ID, "id")
	c.equal(1, got.AccountID, "account")
	c.equal(account.PurposeEmailChange, got.Purpose, "purpose")
	c.equal("new@example.test", got.Target, "target")
	c.equal(strings.Repeat("a", 64), got.Hash, "hash")
	c.equal(0, got.Attempts, "attempts")
	c.equal("10.0.0.1", got.IP, "ip")
	c.same(base, got.CreatedAt, "created at")
	c.same(base.Add(15*time.Minute), got.ExpiresAt, "expires at")
	c.true(got.ConsumedAt == nil && got.InvalidatedAt == nil, "a new code is live")

	bare := newCode(1, account.PurposePasswordReset, 0)
	bare.Target, bare.IP = "", ""
	_, err = s.Codes.Issue(ctx(), bare, base)
	c.noErr(err, "issue without a target or an ip")

	got, err = s.Codes.Latest(ctx(), 1, account.PurposePasswordReset)
	c.noErr(err, "latest of another purpose")
	c.equal("", got.Target, "no target reads empty")

	_, err = s.Codes.Latest(ctx(), 2, account.PurposeEmailChange)
	c.isErr(err, account.ErrCodeNotFound, "another account has none")
}

func codesIssueEndsLive(t *testing.T, s Stores) {
	c := check{t}

	first, err := s.Codes.Issue(ctx(), newCode(1, account.PurposeEmailChange, 0), base)
	c.noErr(err, "issue")

	other, err := s.Codes.Issue(ctx(), newCode(1, account.PurposePasswordReset, 0), base)
	c.noErr(err, "issue another purpose")

	second, err := s.Codes.Issue(ctx(), newCode(1, account.PurposeEmailChange, time.Minute), base.Add(time.Minute))
	c.noErr(err, "issue again")
	c.true(second.ID > first.ID, "ids ascend")

	latest, err := s.Codes.Latest(ctx(), 1, account.PurposeEmailChange)
	c.noErr(err, "latest")
	c.equal(second.ID, latest.ID, "the newest is the latest")
	c.true(latest.InvalidatedAt == nil, "the new one is live")

	// The first one was ended by the second: verifying it is a conflict.
	c.isErr(s.Codes.SaveVerification(ctx(), first, 0, base.Add(2*time.Minute)), account.ErrCodeConflict, "the superseded code is not live")

	// Another purpose is left alone.
	consumed := other
	at := base.Add(time.Minute)
	consumed.ConsumedAt = &at
	c.noErr(s.Codes.SaveVerification(ctx(), consumed, 0, at), "the other purpose's code is still live")
}

func codesIssuedSince(t *testing.T, s Stores) {
	c := check{t}

	for i := range 3 {
		_, err := s.Codes.Issue(ctx(), newCode(1, account.PurposeEmailChange, time.Duration(i)*10*time.Minute), base.Add(time.Duration(i)*10*time.Minute))
		c.noErr(err, "issue")
	}

	_, err := s.Codes.Issue(ctx(), newCode(1, account.PurposePasswordReset, 0), base)
	c.noErr(err, "issue another purpose")

	n, oldest, err := s.Codes.IssuedSince(ctx(), 1, account.PurposeEmailChange, base.Add(-time.Second))
	c.noErr(err, "since")
	c.equal(3, n, "all three")
	c.same(base, oldest, "the oldest")

	n, oldest, err = s.Codes.IssuedSince(ctx(), 1, account.PurposeEmailChange, base.Add(5*time.Minute))
	c.noErr(err, "since")
	c.equal(2, n, "the two after")
	c.same(base.Add(10*time.Minute), oldest, "the oldest of those")

	n, oldest, err = s.Codes.IssuedSince(ctx(), 1, account.PurposeEmailChange, base.Add(time.Hour))
	c.noErr(err, "since")
	c.equal(0, n, "none")
	c.true(oldest.IsZero(), "no oldest")

	n, _, err = s.Codes.IssuedSince(ctx(), 2, account.PurposeEmailChange, base)
	c.noErr(err, "since")
	c.equal(0, n, "another account")
}

func codesSaveVerification(t *testing.T, s Stores) {
	c := check{t}

	issued, err := s.Codes.Issue(ctx(), newCode(1, account.PurposeEmailChange, 0), base)
	c.noErr(err, "issue")

	at := base.Add(time.Minute)
	issued.Attempts, issued.ConsumedAt = 0, &at
	c.noErr(s.Codes.SaveVerification(ctx(), issued, 0, at), "consume")

	got, err := s.Codes.Latest(ctx(), 1, account.PurposeEmailChange)
	c.noErr(err, "latest")
	c.true(got.ConsumedAt != nil, "consumed")
	c.same(at, *got.ConsumedAt, "consumed at")

	c.isErr(s.Codes.SaveVerification(ctx(), issued, 0, at), account.ErrCodeConflict, "a consumed code cannot be saved again")
	c.isErr(s.Codes.SaveVerification(ctx(), account.Code{ID: 999}, 0, at), account.ErrCodeConflict, "an unknown code")
}

func codesConsumedOnce(t *testing.T, s Stores) {
	c := check{t}

	issued, err := s.Codes.Issue(ctx(), newCode(1, account.PurposeEmailChange, 0), base)
	c.noErr(err, "issue")

	const workers = 8

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
		errs []error
	)

	for range workers {
		wg.Go(func() {
			read, err := s.Codes.Latest(ctx(), 1, account.PurposeEmailChange)
			if err == nil {
				at := base.Add(time.Minute)
				read.ConsumedAt = &at
				err = s.Codes.SaveVerification(ctx(), read, issued.Attempts, at)
			}

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				wins++
			} else {
				errs = append(errs, err)
			}
		})
	}

	wg.Wait()

	c.equal(1, wins, "exactly one verification consumes the code")

	for _, err := range errs {
		c.isErr(err, account.ErrCodeConflict, "the others conflict")
	}
}

func codesAttemptsCounted(t *testing.T, s Stores) {
	c := check{t}

	issued, err := s.Codes.Issue(ctx(), newCode(1, account.PurposeEmailChange, 0), base)
	c.noErr(err, "issue")

	// Two wrong guesses that read the same state: one is saved, the other conflicts and must read again.
	first, second := issued, issued
	first.Attempts, second.Attempts = 1, 1

	c.noErr(s.Codes.SaveVerification(ctx(), first, 0, base), "the first guess")
	c.isErr(s.Codes.SaveVerification(ctx(), second, 0, base), account.ErrCodeConflict, "the second read a stale state")

	read, err := s.Codes.Latest(ctx(), 1, account.PurposeEmailChange)
	c.noErr(err, "latest")
	c.equal(1, read.Attempts, "one attempt used")

	read.Attempts = 2
	at := base.Add(time.Second)
	read.InvalidatedAt = &at
	c.noErr(s.Codes.SaveVerification(ctx(), read, 1, at), "the guess that ends the code")

	read, err = s.Codes.Latest(ctx(), 1, account.PurposeEmailChange)
	c.noErr(err, "latest")
	c.equal(2, read.Attempts, "attempts")
	c.true(read.InvalidatedAt != nil, "invalidated")
}

func codesInvalidateLive(t *testing.T, s Stores) {
	c := check{t}

	issued, err := s.Codes.Issue(ctx(), newCode(1, account.PurposeEmailChange, 0), base)
	c.noErr(err, "issue")

	other, err := s.Codes.Issue(ctx(), newCode(2, account.PurposeEmailChange, 0), base)
	c.noErr(err, "issue for another account")

	c.noErr(s.Codes.InvalidateLive(ctx(), 1, account.PurposeEmailChange, base.Add(time.Minute)), "invalidate")
	c.noErr(s.Codes.InvalidateLive(ctx(), 1, account.PurposeEmailChange, base.Add(time.Minute)), "invalidating again is fine")

	got, err := s.Codes.Latest(ctx(), 1, account.PurposeEmailChange)
	c.noErr(err, "latest")
	c.equal(issued.ID, got.ID, "the code is still the latest")
	c.true(got.InvalidatedAt != nil, "but ended")
	c.same(base.Add(time.Minute), *got.InvalidatedAt, "ended at")

	got, err = s.Codes.Latest(ctx(), 2, account.PurposeEmailChange)
	c.noErr(err, "latest")
	c.equal(other.ID, got.ID, "another account")
	c.true(got.InvalidatedAt == nil, "its code is untouched")
}

func reauthFindCreateSave(t *testing.T, s Stores) {
	c := check{t}

	_, err := s.Reauth.Find(ctx(), 1)
	c.isErr(err, account.ErrReauthNotFound, "no counter yet")

	first := account.Attempts{AccountID: 1, Failures: 1, UpdatedAt: base}
	c.noErr(s.Reauth.Create(ctx(), first), "create")
	c.isErr(s.Reauth.Create(ctx(), first), account.ErrReauthConflict, "a second create loses")

	got, err := s.Reauth.Find(ctx(), 1)
	c.noErr(err, "find")
	c.equal(1, got.Failures, "failures")
	c.true(got.LockedUntil == nil, "not locked")
	c.same(base, got.UpdatedAt, "updated at")

	until := base.Add(15 * time.Minute)
	locked := account.Attempts{AccountID: 1, Failures: 5, LockedUntil: &until, UpdatedAt: base.Add(time.Minute)}
	c.noErr(s.Reauth.Save(ctx(), locked, first), "save over what was read")
	c.isErr(s.Reauth.Save(ctx(), locked, first), account.ErrReauthConflict, "a stale read loses")

	got, err = s.Reauth.Find(ctx(), 1)
	c.noErr(err, "find")
	c.equal(5, got.Failures, "failures")
	c.true(got.LockedUntil != nil, "locked")
	c.same(until, *got.LockedUntil, "locked until")

	cleared := account.Attempts{AccountID: 1, UpdatedAt: base.Add(2 * time.Minute)}
	c.noErr(s.Reauth.Save(ctx(), cleared, got), "clear, matching the lock as read")

	got, err = s.Reauth.Find(ctx(), 1)
	c.noErr(err, "find")
	c.equal(0, got.Failures, "cleared")
	c.true(got.LockedUntil == nil, "unlocked")

	c.isErr(s.Reauth.Save(ctx(), cleared, account.Attempts{AccountID: 9, Failures: 1}), account.ErrReauthConflict, "no counter to save over")
}

func reauthOneWins(t *testing.T, s Stores) {
	c := check{t}

	c.noErr(s.Reauth.Create(ctx(), account.Attempts{AccountID: 1, Failures: 1, UpdatedAt: base}), "create")
	read := account.Attempts{AccountID: 1, Failures: 1, UpdatedAt: base}

	const workers = 8

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
		errs []error
	)

	for range workers {
		wg.Go(func() {
			err := s.Reauth.Save(ctx(), account.Attempts{AccountID: 1, Failures: 2, UpdatedAt: base.Add(time.Second)}, read)

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				wins++
			} else {
				errs = append(errs, err)
			}
		})
	}

	wg.Wait()

	c.equal(1, wins, "exactly one request counts from one read")

	for _, err := range errs {
		c.isErr(err, account.ErrReauthConflict, "the others conflict")
	}
}
