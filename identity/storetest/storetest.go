// Package storetest is the conformance suite every identity store must pass:
// the account, sign-in history and session stores of identity/account.
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

// Stores are the three stores under test, over one database.
type Stores struct {
	Accounts account.AccountStore
	SignIns  account.SignInLog
	Sessions account.SessionStore
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
