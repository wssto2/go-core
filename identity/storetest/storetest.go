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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wssto2/go-core/identity/account"
)

// Stores are the stores under test, over one database.
type Stores struct {
	Accounts interface {
		account.Store
		account.Searcher
	}
	SignIns  account.SignInHistory
	Sessions account.SessionStore
	Codes    account.CodeStore
	Reauth   account.ReauthStore
	Changes  account.ChangeLog
	// Activity reads what people did; it must read the changes Changes records and the
	// sessions Sessions opens (record type "account"). Left nil, its checks are skipped.
	Activity account.ActivityLog
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
		"accounts/find by email":         accountsFindByEmail,
		"accounts/update":                accountsUpdate,
		"accounts/update login taken":    accountsUpdateLoginTaken,
		"accounts/email is unique":       accountsEmailUnique,
		"accounts/search":                accountsSearch,
		"accounts/search escapes":        accountsSearchEscapes,
		"accounts/counts":                accountsCounts,
		"signins/entries":                signInsEntries,
		"signins/entries by event":       signInsByEvent,
		"signins/last sign-ins":          signInsLast,
		"signins/wrong passwords since":  signInsWrongSince,
		"changes/record and list":        changesRecordAndList,
		"changes/by action":              changesByAction,
		"activity/page and filters":      activityPageAndFilters,
		"activity/an ended session":      activityEndedSession,
		"activity/counts per type":       activityByType,
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
		Login: login, Email: strings.ToLower(login) + "@example.test", Phone: "+385 1 555 " + login, Name: strings.ToUpper(login[:1]) + login[1:], Locale: "hr",
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
	c.equal("+385 1 555 Ana", byID.Phone, "phone")
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

func entry(accountID int, e account.SignInEvent, at time.Duration) account.SignInEntry {
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

	var events []account.SignInEvent
	for _, e := range got {
		events = append(events, e.Event)
	}

	c.equal([]account.SignInEvent{account.Unlocked, account.WrongPassword, account.SignedIn}, events, "the latest three lock events, newest first")

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

func accountsFindByEmail(t *testing.T, s Stores) {
	c := check{t}

	a, err := s.Accounts.Create(ctx(), newAccount("ana"))
	c.noErr(err, "create")

	got, err := s.Accounts.FindByEmail(ctx(), " ANA@Example.test ")
	c.noErr(err, "find by email")
	c.equal(a.ID, got.ID, "the address is compared without case or space")

	_, err = s.Accounts.FindByEmail(ctx(), "nobody@example.test")
	c.isErr(err, account.ErrNotFound, "no such address")
}

func str(s string) *string { return &s }

func accountsUpdate(t *testing.T, s Stores) {
	c := check{t}

	a, err := s.Accounts.Create(ctx(), newAccount("ana"))
	c.noErr(err, "create")

	c.noErr(s.Accounts.Update(ctx(), a.ID, account.Changes{Name: str("Ana Anić"), Phone: str("")}), "update some fields")

	got, err := s.Accounts.Find(ctx(), a.ID)
	c.noErr(err, "find")
	c.equal("Ana Anić", got.Name, "name")
	c.equal("", got.Phone, "phone cleared")
	c.equal("ana", got.Login, "a field not set is left alone")
	c.equal("ana@example.test", got.Email, "email left alone")
	c.equal("hash-ana", got.PasswordHash, "the password hash is not touched")
	c.true(got.Active, "active left alone")

	c.noErr(s.Accounts.Update(ctx(), a.ID, account.Changes{Login: str(" NEW "), Email: str("new@example.test"), Locale: str("en")}), "update login, email and locale")
	c.noErr(s.Accounts.Update(ctx(), a.ID, account.Changes{Locale: str("en")}), "update to what it is: not a missing account")
	c.noErr(s.Accounts.Update(ctx(), a.ID, account.Changes{}), "nothing to update")

	got, err = s.Accounts.Find(ctx(), a.ID)
	c.noErr(err, "find")
	c.equal("new", got.Login, "the login is stored normalised")
	c.equal("new@example.test", got.Email, "email")
	c.equal("en", got.Locale, "locale")

	_, err = s.Accounts.FindByLogin(ctx(), "ana")
	c.isErr(err, account.ErrNotFound, "the old login is free")

	c.isErr(s.Accounts.Update(ctx(), 999, account.Changes{Name: str("x")}), account.ErrNotFound, "a missing account")
	c.isErr(s.Accounts.Update(ctx(), 999, account.Changes{}), account.ErrNotFound, "a missing account, nothing to write")
}

func accountsUpdateLoginTaken(t *testing.T, s Stores) {
	c := check{t}

	a, _ := s.Accounts.Create(ctx(), newAccount("ana"))
	_, err := s.Accounts.Create(ctx(), newAccount("boris"))
	c.noErr(err, "create")

	c.isErr(s.Accounts.Update(ctx(), a.ID, account.Changes{Login: str("BORIS")}), account.ErrLoginTaken, "a login in use")
	c.noErr(s.Accounts.Update(ctx(), a.ID, account.Changes{Login: str("ANA")}), "its own login, in another case")
}

func seedForSearch(c check, s Stores) []account.Account {
	c.t.Helper()

	var out []account.Account

	for i, in := range []struct {
		login, name, email string
		active             bool
	}{
		{"ana", "Ana Horvat", "ana@example.test", true},
		{"boris", "Boris Kovač", "boris@firma.test", true},
		{"cvita", "Cvita Babić", "cvita@example.test", false},
		{"dino", "Dino 100%", "dino@example.test", true},
		{"eva", "Eva_Marić", "eva@example.test", true},
	} {
		a := newAccount(in.login)
		a.Name, a.Email, a.Active, a.CreatedAt = in.name, in.email, in.active, base.Add(time.Duration(i)*time.Hour)

		created, err := s.Accounts.Create(ctx(), a)
		c.noErr(err, "create")

		if !in.active {
			c.noErr(s.Accounts.SetActive(ctx(), created.ID, false), "deactivate")
		}

		out = append(out, created)
	}

	return out
}

func logins(page account.Page) []string {
	var out []string
	for _, a := range page.Accounts {
		out = append(out, a.Login)
	}

	return out
}

func accountsSearch(t *testing.T, s Stores) {
	c := check{t}
	people := seedForSearch(c, s)

	yes, no := true, false

	for name, tc := range map[string]struct {
		q     account.Query
		want  []string
		total int
	}{
		"everyone, by login":     {account.Query{Page: 1, PerPage: 10}, []string{"ana", "boris", "cvita", "dino", "eva"}, 5},
		"descending":             {account.Query{OrderBy: account.OrderLogin, Desc: true, Page: 1, PerPage: 10}, []string{"eva", "dino", "cvita", "boris", "ana"}, 5},
		"by name":                {account.Query{OrderBy: account.OrderName, Page: 1, PerPage: 10}, []string{"ana", "boris", "cvita", "dino", "eva"}, 5},
		"by e-mail":              {account.Query{OrderBy: account.OrderEmail, Page: 1, PerPage: 10}, []string{"ana", "boris", "cvita", "dino", "eva"}, 5},
		"newest first":           {account.Query{OrderBy: account.OrderCreated, Desc: true, Page: 1, PerPage: 10}, []string{"eva", "dino", "cvita", "boris", "ana"}, 5},
		"a page":                 {account.Query{Page: 2, PerPage: 2}, []string{"cvita", "dino"}, 5},
		"past the end":           {account.Query{Page: 9, PerPage: 2}, nil, 5},
		"search the login":       {account.Query{Search: "BOR", Page: 1, PerPage: 10}, []string{"boris"}, 1},
		"search the name":        {account.Query{Search: "kovač", Page: 1, PerPage: 10}, []string{"boris"}, 1},
		"search the e-mail":      {account.Query{Search: "firma", Page: 1, PerPage: 10}, []string{"boris"}, 1},
		"search finds several":   {account.Query{Search: "example", Page: 1, PerPage: 10}, []string{"ana", "cvita", "dino", "eva"}, 4},
		"search finds nobody":    {account.Query{Search: "zzz", Page: 1, PerPage: 10}, nil, 0},
		"active only":            {account.Query{Active: &yes, Page: 1, PerPage: 10}, []string{"ana", "boris", "dino", "eva"}, 4},
		"inactive only":          {account.Query{Active: &no, Page: 1, PerPage: 10}, []string{"cvita"}, 1},
		"locked ones":            {account.Query{Locked: &yes, LockedIDs: []int{people[0].ID, people[1].ID}, Page: 1, PerPage: 10}, []string{"ana", "boris"}, 2},
		"locked, nobody":         {account.Query{Locked: &yes, Page: 1, PerPage: 10}, nil, 0},
		"not locked":             {account.Query{Locked: &no, LockedIDs: []int{people[0].ID}, Page: 1, PerPage: 10}, []string{"boris", "cvita", "dino", "eva"}, 4},
		"not locked, nobody is":  {account.Query{Locked: &no, Page: 1, PerPage: 10}, []string{"ana", "boris", "cvita", "dino", "eva"}, 5},
		"search with a status":   {account.Query{Search: "example", Active: &yes, Page: 1, PerPage: 10}, []string{"ana", "dino", "eva"}, 3},
		"an unknown order falls": {account.Query{OrderBy: "nonsense", Page: 1, PerPage: 10}, []string{"ana", "boris", "cvita", "dino", "eva"}, 5},
	} {
		got, err := s.Accounts.Search(ctx(), tc.q)
		c.noErr(err, name)
		c.equal(tc.want, logins(got), name+": rows")
		c.equal(tc.total, got.Total, name+": total counts every match, not the page")
	}
}

func accountsCounts(t *testing.T, s Stores) {
	c := check{t}
	people := seedForSearch(c, s)

	for name, tc := range map[string]struct {
		q    account.Query
		want account.StatusCounts
	}{
		"nobody locked":          {account.Query{}, account.StatusCounts{Active: 4, Inactive: 1}},
		"two locked":             {account.Query{LockedIDs: []int{people[0].ID, people[1].ID}}, account.StatusCounts{Active: 2, Locked: 2, Inactive: 1}},
		"an inactive one locked": {account.Query{LockedIDs: []int{people[2].ID}}, account.StatusCounts{Active: 4, Inactive: 1}},
		"under a search":         {account.Query{Search: "example", LockedIDs: []int{people[0].ID, people[1].ID}}, account.StatusCounts{Active: 2, Locked: 1, Inactive: 1}},
		"a search finds nobody":  {account.Query{Search: "zzz", LockedIDs: []int{people[0].ID}}, account.StatusCounts{}},
		"the page is ignored":    {account.Query{Page: 9, PerPage: 1}, account.StatusCounts{Active: 4, Inactive: 1}},
	} {
		got, err := s.Accounts.Counts(ctx(), tc.q)
		c.noErr(err, name)
		c.equal(tc.want, got, name)
	}
}

func accountsSearchEscapes(t *testing.T, s Stores) {
	c := check{t}
	seedForSearch(c, s)

	for term, want := range map[string][]string{"%": {"dino"}, "_": {"eva"}, "100%": {"dino"}, "!": nil, "a_a": nil} {
		got, err := s.Accounts.Search(ctx(), account.Query{Search: term, Page: 1, PerPage: 10})
		c.noErr(err, "search "+term)
		c.equal(want, logins(got), "a wildcard in the term is itself: "+term)
	}
}

func signInsEntries(t *testing.T, s Stores) {
	c := check{t}

	for i := range 5 {
		c.noErr(s.SignIns.Record(ctx(), entry(1, account.SignedIn, time.Duration(i)*time.Second)), "record")
	}

	c.noErr(s.SignIns.Record(ctx(), entry(2, account.SignedIn, 9*time.Second)), "another account")

	got, total, err := s.SignIns.Entries(ctx(), account.SignInQuery{AccountID: 1, Limit: 2})
	c.noErr(err, "entries")
	c.equal(5, total, "every row of the account")
	c.equal(2, len(got), "a page")
	c.same(base.Add(4*time.Second), got[0].CreatedAt, "newest first")
	c.same(base.Add(3*time.Second), got[1].CreatedAt, "newest first")

	got, _, err = s.SignIns.Entries(ctx(), account.SignInQuery{AccountID: 1, Offset: 4, Limit: 2})
	c.noErr(err, "entries")
	c.equal(1, len(got), "the last page")

	got, total, err = s.SignIns.Entries(ctx(), account.SignInQuery{AccountID: 3, Limit: 2})
	c.noErr(err, "entries")
	c.equal(0, total+len(got), "an account without history")
}

func signInsByEvent(t *testing.T, s Stores) {
	c := check{t}

	for i, e := range []account.SignInEvent{account.SignedIn, account.WrongPassword, account.WrongPassword, account.LockedOut, account.Unlocked} {
		c.noErr(s.SignIns.Record(ctx(), entry(1, e, time.Duration(i)*time.Second)), "record")
	}

	c.noErr(s.SignIns.Record(ctx(), entry(2, account.WrongPassword, 9*time.Second)), "another account")

	failed := []account.SignInEvent{account.WrongPassword, account.LockedOut}

	got, total, err := s.SignIns.Entries(ctx(), account.SignInQuery{AccountID: 1, Events: failed, Limit: 2})
	c.noErr(err, "entries")
	c.equal(3, total, "every failed row, not the page")
	c.equal(2, len(got), "a page")
	c.equal(account.LockedOut, got[0].Event, "newest first")

	got, total, err = s.SignIns.Entries(ctx(), account.SignInQuery{AccountID: 1, Events: []account.SignInEvent{account.Unlocked}, Limit: 10})
	c.noErr(err, "entries")
	c.equal(1, total, "one event")

	counts, err := s.SignIns.EventCounts(ctx(), 1)
	c.noErr(err, "counts")
	c.equal(map[account.SignInEvent]int{account.SignedIn: 1, account.WrongPassword: 2, account.LockedOut: 1, account.Unlocked: 1}, counts, "a count per event, the account's own")

	none, err := s.SignIns.EventCounts(ctx(), 7)
	c.noErr(err, "counts")
	c.equal(0, len(none), "an account without history")
}

func signInsLast(t *testing.T, s Stores) {
	c := check{t}

	for _, e := range []account.SignInEntry{
		entry(1, account.SignedIn, 1*time.Second),
		entry(1, account.WrongPassword, 9*time.Second), // not a sign-in
		entry(1, account.SignedIn, 5*time.Second),
		entry(2, account.WrongPassword, 2*time.Second), // never signed in
		entry(3, account.SignedIn, 3*time.Second),
	} {
		c.noErr(s.SignIns.Record(ctx(), e), "record")
	}

	got, err := s.SignIns.LastSignIns(ctx(), []int{1, 2, 3})
	c.noErr(err, "last sign-ins")
	c.equal(2, len(got), "only who has one")
	c.same(base.Add(5*time.Second), got[1], "the latest sign-in of 1")
	c.same(base.Add(3*time.Second), got[3], "the sign-in of 3")

	got, err = s.SignIns.LastSignIns(ctx(), []int{1})
	c.noErr(err, "last sign-ins")
	c.equal(1, len(got), "only who is asked for")

	got, err = s.SignIns.LastSignIns(ctx(), nil)
	c.noErr(err, "last sign-ins")
	c.equal(0, len(got), "nobody asked for")
}

func signInsWrongSince(t *testing.T, s Stores) {
	c := check{t}

	for _, e := range []account.SignInEntry{
		entry(1, account.WrongPassword, 1*time.Minute),
		entry(1, account.WrongPassword, 2*time.Minute),
		entry(2, account.WrongPassword, 10*time.Minute),
		entry(3, account.SignedIn, 11*time.Minute),
		entry(4, account.WrongPassword, 30*time.Minute),
	} {
		c.noErr(s.SignIns.Record(ctx(), e), "record")
	}

	got, err := s.SignIns.WrongPasswordsSince(ctx(), base.Add(90*time.Second))
	c.noErr(err, "since")

	slices.Sort(got)
	c.equal([]int{1, 2, 4}, got, "each account once, only wrong passwords, only after the time")

	got, err = s.SignIns.WrongPasswordsSince(ctx(), base.Add(time.Hour))
	c.noErr(err, "since")
	c.equal(0, len(got), "none")
}

func changesRecordAndList(t *testing.T, s Stores) {
	c := check{t}

	c.noErr(s.Changes.Record(ctx(), account.Change{
		AccountID: 1, ActorID: 9, Action: account.ChangeUpdated, Fields: []string{"name", "phone"},
		Before: map[string]string{"name": "Ana", "phone": ""}, After: map[string]string{"name": "Ana Anić", "phone": "123"},
	}), "record")
	c.noErr(s.Changes.Record(ctx(), account.Change{AccountID: 1, Action: account.ChangePassword, Fields: []string{"password"}}), "record a change without values")
	c.noErr(s.Changes.Record(ctx(), account.Change{AccountID: 2, ActorID: 9, Action: account.ChangeCreated}), "another account")

	got, total, err := s.Changes.Changes(ctx(), account.ChangeQuery{AccountID: 1, Limit: 10})
	c.noErr(err, "changes")
	c.equal(2, total, "the account's own")
	c.equal(2, len(got), "rows")
	c.equal(account.ChangePassword, got[0].Action, "newest first")
	c.equal([]string{"password"}, got[0].Fields, "fields")
	c.equal(0, got[0].ActorID, "no actor reads zero")
	c.true(len(got[0].Before) == 0 && len(got[0].After) == 0, "no values")
	c.true(!got[0].At.IsZero(), "stamped")

	c.equal(account.ChangeUpdated, got[1].Action, "action")
	c.equal(1, got[1].AccountID, "account")
	c.equal(9, got[1].ActorID, "actor")
	c.equal([]string{"name", "phone"}, got[1].Fields, "fields")
	c.equal(map[string]string{"name": "Ana", "phone": ""}, got[1].Before, "before")
	c.equal(map[string]string{"name": "Ana Anić", "phone": "123"}, got[1].After, "after")

	page, total, err := s.Changes.Changes(ctx(), account.ChangeQuery{AccountID: 1, Offset: 1, Limit: 1})
	c.noErr(err, "changes")
	c.equal(2, total, "the total is not the page")
	c.equal(1, len(page), "a page")
	c.equal(account.ChangeUpdated, page[0].Action, "the second page")

	none, total, err := s.Changes.Changes(ctx(), account.ChangeQuery{AccountID: 7, Limit: 10})
	c.noErr(err, "changes")
	c.equal(0, total+len(none), "an account without changes")
}

func changesByAction(t *testing.T, s Stores) {
	c := check{t}

	for _, a := range []account.ChangeAction{account.ChangeCreated, account.ChangePassword, account.ChangeUpdated, account.ChangeDeactivated, account.ChangeActivated} {
		c.noErr(s.Changes.Record(ctx(), account.Change{AccountID: 1, Action: a, Fields: []string{"x"}}), "record")
	}

	c.noErr(s.Changes.Record(ctx(), account.Change{AccountID: 2, Action: account.ChangePassword}), "another account")

	access := []account.ChangeAction{account.ChangePassword, account.ChangeDeactivated, account.ChangeActivated}

	got, total, err := s.Changes.Changes(ctx(), account.ChangeQuery{AccountID: 1, Only: access, Limit: 2})
	c.noErr(err, "only")
	c.equal(3, total, "every access change, not the page")
	c.equal(account.ChangeActivated, got[0].Action, "newest first")

	got, total, err = s.Changes.Changes(ctx(), account.ChangeQuery{AccountID: 1, Except: access, Limit: 10})
	c.noErr(err, "except")
	c.equal(2, total, "the rest")
	c.equal([]account.ChangeAction{account.ChangeUpdated, account.ChangeCreated}, []account.ChangeAction{got[0].Action, got[1].Action}, "the details")

	counts, err := s.Changes.ChangeCounts(ctx(), 1)
	c.noErr(err, "counts")
	c.equal(map[account.ChangeAction]int{
		account.ChangeCreated: 1, account.ChangePassword: 1, account.ChangeUpdated: 1, account.ChangeDeactivated: 1, account.ChangeActivated: 1,
	}, counts, "a count per action, the account's own")

	none, err := s.Changes.ChangeCounts(ctx(), 7)
	c.noErr(err, "counts")
	c.equal(0, len(none), "an account without changes")
}

func accountsEmailUnique(t *testing.T, s Stores) {
	c := check{t}

	a := newAccount("ana")
	a.Email = "shared@example.test"
	first, err := s.Accounts.Create(ctx(), a)
	c.noErr(err, "create")

	b := newAccount("boris")
	b.Email = "Shared@Example.test"
	_, err = s.Accounts.Create(ctx(), b)
	c.isErr(err, account.ErrEmailTaken, "the same address, in another case")

	b.Email = "boris@example.test"
	second, err := s.Accounts.Create(ctx(), b)
	c.noErr(err, "another address")

	c.isErr(s.Accounts.Update(ctx(), second.ID, account.Changes{Email: str("SHARED@example.test")}), account.ErrEmailTaken, "update to a taken address")
	c.noErr(s.Accounts.Update(ctx(), first.ID, account.Changes{Email: str("shared@example.test")}), "an account keeps its own address")

	got, err := s.Accounts.Find(ctx(), second.ID)
	c.noErr(err, "find")
	c.equal("boris@example.test", got.Email, "the refused update wrote nothing")

	// no address is no collision: any number of accounts may have none
	for _, login := range []string{"cvita", "dino"} {
		n := newAccount(login)
		n.Email = ""
		_, err := s.Accounts.Create(ctx(), n)
		c.noErr(err, "create without an address: "+login)
	}

	none, err := s.Accounts.FindByLogin(ctx(), "dino")
	c.noErr(err, "find")
	c.equal("", none.Email, "no address reads empty")
	c.noErr(s.Accounts.Update(ctx(), none.ID, account.Changes{Email: str("")}), "clearing an address")
}

func activityPageAndFilters(t *testing.T, s Stores) {
	if s.Activity == nil {
		t.Skip("no ActivityLog")
	}

	c := check{t}

	for _, ch := range []account.Change{
		{AccountID: 1, ActorID: 7, Action: account.ChangeCreated},
		{AccountID: 2, ActorID: 8, Action: account.ChangeUpdated},
		{AccountID: 3, ActorID: 7, Action: account.ChangeUpdated, Fields: []string{"name"}},
		{AccountID: 4, ActorID: 7, Action: account.ChangePassword, Fields: []string{"password"}},
	} {
		c.noErr(s.Changes.Record(ctx(), ch), "record")
	}

	got, total, err := s.Activity.Activity(ctx(), account.ActivityQuery{ActorID: 7, Limit: 2})
	c.noErr(err, "activity")
	c.equal(3, total, "what actor 7 did, not actor 8")
	c.equal(2, len(got), "a page")
	c.equal(4, got[0].RecordID, "newest first")
	c.equal("account", got[0].RecordType, "record type")
	c.equal(account.ActivityChanged, got[0].Action, "a password change is a change")
	c.equal(3, got[1].RecordID, "newest first")
	c.equal(0, got[0].SignedInAs, "nobody was signed in as them")
	c.true(!got[0].At.IsZero(), "stamped")

	got, total, err = s.Activity.Activity(ctx(), account.ActivityQuery{ActorID: 7, Offset: 2, Limit: 2})
	c.noErr(err, "activity")
	c.equal(3, total, "the total is not the page")
	c.equal(1, len(got), "the last page")
	c.equal(account.ActivityCreated, got[0].Action, "created")

	for name, q := range map[string]account.ActivityQuery{
		"within the type":        {Within: account.RecordSet{Types: []string{"account"}}},
		"within a prefix":        {Within: account.RecordSet{Prefixes: []string{"acc"}}},
		"outside another type":   {Outside: account.RecordSet{Types: []string{"order"}}},
		"outside another prefix": {Outside: account.RecordSet{Prefixes: []string{"ord"}}},
	} {
		q.ActorID, q.Limit = 7, 10
		_, n, err := s.Activity.Activity(ctx(), q)
		c.noErr(err, name)
		c.equal(3, n, name)
	}

	for name, q := range map[string]account.ActivityQuery{
		"within another type":   {Within: account.RecordSet{Types: []string{"order"}}},
		"within another prefix": {Within: account.RecordSet{Prefixes: []string{"ord"}}},
		"outside the type":      {Outside: account.RecordSet{Types: []string{"account"}}},
		"outside the prefix":    {Outside: account.RecordSet{Prefixes: []string{"acc"}}},
		"a person who did none": {ActorID: 99},
	} {
		if q.ActorID == 0 {
			q.ActorID = 7
		}

		q.Limit = 10
		_, n, err := s.Activity.Activity(ctx(), q)
		c.noErr(err, name)
		c.equal(0, n, name)
	}
}

func activityEndedSession(t *testing.T, s Stores) {
	if s.Activity == nil {
		t.Skip("no ActivityLog")
	}

	c := check{t}

	// Somebody (3) signs in as person 7 and 7's own session is a second one: only the first marks.
	open(c, s, 7, 3, "as 7", time.Now().UTC().Add(-time.Hour).Truncate(time.Second))
	c.noErr(s.Changes.Record(ctx(), account.Change{AccountID: 1, ActorID: 7, Action: account.ChangeUpdated}), "record")

	got, _, err := s.Activity.Activity(ctx(), account.ActivityQuery{ActorID: 7, Limit: 10})
	c.noErr(err, "activity")
	c.equal(1, len(got), "rows")
	c.equal(0, got[0].SignedInAs, "the session opened an hour ago was last used then: it ended")
}

func activityByType(t *testing.T, s Stores) {
	if s.Activity == nil {
		t.Skip("no ActivityLog")
	}

	c := check{t}

	for _, ch := range []account.Change{
		{AccountID: 1, ActorID: 7, Action: account.ChangeCreated},
		{AccountID: 2, ActorID: 7, Action: account.ChangeUpdated},
		{AccountID: 3, ActorID: 8, Action: account.ChangeUpdated},
	} {
		c.noErr(s.Changes.Record(ctx(), ch), "record")
	}

	got, err := s.Activity.ActivityByType(ctx(), account.ActivityQuery{ActorID: 7})
	c.noErr(err, "counts")
	c.equal(map[string]int{"account": 2}, got, "what actor 7 did, per type")

	got, err = s.Activity.ActivityByType(ctx(), account.ActivityQuery{ActorID: 7, To: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)})
	c.noErr(err, "counts")
	c.equal(0, len(got), "nothing before the year 2000")

	got, err = s.Activity.ActivityByType(ctx(), account.ActivityQuery{ActorID: 99})
	c.noErr(err, "counts")
	c.equal(0, len(got), "a person who did none")
}
