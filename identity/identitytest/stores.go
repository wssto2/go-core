package identitytest

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wssto2/go-core/identity/account"
)

// Accounts is a memory account.Store.
type Accounts struct {
	mu   sync.Mutex
	next int
	rows map[int]account.Account
}

// NewAccounts returns an empty store.
func NewAccounts() *Accounts { return &Accounts{next: 1, rows: map[int]account.Account{}} }

// Find implements account.Store.
func (s *Accounts) Find(_ context.Context, id int) (account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.rows[id]
	if !ok {
		return account.Account{}, account.ErrNotFound
	}

	return a, nil
}

// FindByLogin implements account.Store.
func (s *Accounts) FindByLogin(_ context.Context, login string) (account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, a := range s.rows {
		if a.Login == account.NormalizeLogin(login) {
			return a, nil
		}
	}

	return account.Account{}, account.ErrNotFound
}

// FindMany implements account.Store.
func (s *Accounts) FindMany(_ context.Context, ids []int) ([]account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []account.Account

	for _, id := range ids {
		if a, ok := s.rows[id]; ok {
			out = append(out, a)
		}
	}

	return out, nil
}

// Create implements account.Store. An account that comes with an ID
// keeps it, which lets a test name its people.
func (s *Accounts) Create(_ context.Context, a account.Account) (account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a.Login = account.NormalizeLogin(a.Login)

	for _, other := range s.rows {
		if other.Login == a.Login {
			return account.Account{}, account.ErrLoginTaken
		}
	}

	a.Email = account.NormalizeEmail(a.Email)
	if s.emailUsed(a.Email, 0) {
		return account.Account{}, account.ErrEmailTaken
	}

	if a.ID == 0 {
		a.ID = s.next
	}

	s.next = max(s.next, a.ID) + 1
	s.rows[a.ID] = a

	return a, nil
}

// emailUsed reports whether another account has the address; none is never used.
func (s *Accounts) emailUsed(email string, except int) bool {
	if email == "" {
		return false
	}

	for _, o := range s.rows {
		if o.ID != except && o.Email == email {
			return true
		}
	}

	return false
}

func (s *Accounts) update(id int, fn func(*account.Account)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.rows[id]
	if !ok {
		return account.ErrNotFound
	}

	fn(&a)
	s.rows[id] = a

	return nil
}

// SetLocale implements account.Store.
func (s *Accounts) SetLocale(_ context.Context, id int, locale string) error {
	return s.update(id, func(a *account.Account) { a.Locale = locale })
}

// SetPasswordHash implements account.Store.
func (s *Accounts) SetPasswordHash(_ context.Context, id int, hash string) error {
	return s.update(id, func(a *account.Account) { a.PasswordHash = hash })
}

// SetActive implements account.Store.
func (s *Accounts) SetActive(_ context.Context, id int, active bool) error {
	return s.update(id, func(a *account.Account) { a.Active = active })
}

// FindByEmail implements account.Store.
func (s *Accounts) FindByEmail(_ context.Context, email string) (account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, a := range s.rows {
		if strings.EqualFold(a.Email, strings.TrimSpace(email)) {
			return a, nil
		}
	}

	return account.Account{}, account.ErrNotFound
}

// Update implements account.Store.
func (s *Accounts) Update(_ context.Context, id int, c account.Changes) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.rows[id]
	if !ok {
		return account.ErrNotFound
	}

	if c.Login != nil {
		login := account.NormalizeLogin(*c.Login)

		for _, other := range s.rows {
			if other.ID != id && other.Login == login {
				return account.ErrLoginTaken
			}
		}

		a.Login = login
	}

	set := func(field *string, v *string) {
		if v != nil {
			*field = *v
		}
	}

	if c.Email != nil {
		email := account.NormalizeEmail(*c.Email)
		if s.emailUsed(email, id) {
			return account.ErrEmailTaken
		}

		a.Email = email
	}

	set(&a.Name, c.Name)
	set(&a.Phone, c.Phone)
	set(&a.Locale, c.Locale)

	s.rows[id] = a

	return nil
}

// Search implements account.Searcher.
func (s *Accounts) Search(_ context.Context, q account.Query) (account.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	term := strings.ToLower(q.Search)

	var rows []account.Account

	for _, a := range s.rows {
		if term != "" && !strings.Contains(strings.ToLower(a.Login+"\x00"+a.Name+"\x00"+a.Email), term) {
			continue
		}

		if q.Active != nil && a.Active != *q.Active {
			continue
		}

		if q.Locked != nil && slices.Contains(q.LockedIDs, a.ID) != *q.Locked {
			continue
		}

		rows = append(rows, a)
	}

	key := func(a account.Account) string {
		switch q.OrderBy {
		case account.OrderName:
			return strings.ToLower(a.Name)
		case account.OrderEmail:
			return strings.ToLower(a.Email)
		case account.OrderCreated:
			return a.CreatedAt.Format(time.RFC3339Nano)
		default:
			return a.Login
		}
	}

	slices.SortFunc(rows, func(a, b account.Account) int {
		c := strings.Compare(key(a), key(b))
		if c == 0 {
			c = a.ID - b.ID
		}

		if q.Desc {
			return -c
		}

		return c
	})

	page := account.Page{Total: len(rows)}
	perPage := max(q.PerPage, 1)
	from := min((max(q.Page, 1)-1)*perPage, len(rows))
	page.Accounts = rows[from:min(from+perPage, len(rows))]

	return page, nil
}

// SignIns is a memory account.SignInLog.
type SignIns struct {
	mu   sync.Mutex
	rows []account.SignInEntry
}

// NewSignIns returns an empty history.
func NewSignIns() *SignIns { return &SignIns{} }

// Record implements account.SignInLog.
func (s *SignIns) Record(_ context.Context, row account.SignInEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	row.ID = len(s.rows) + 1
	s.rows = append(s.rows, row)

	return nil
}

// LockEvents implements account.SignInLog.
func (s *SignIns) LockEvents(_ context.Context, accountID, n int) ([]account.SignInEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []account.SignInEntry

	for _, r := range slices.Backward(s.rows) {
		if r.AccountID == accountID && account.LockEvent(r.Event) {
			out = append(out, r)
			if len(out) == n {
				break
			}
		}
	}

	return out, nil
}

// Entries implements account.SignInHistory.
func (s *SignIns) Entries(_ context.Context, accountID, offset, limit int) ([]account.SignInEntry, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var mine []account.SignInEntry

	for _, r := range slices.Backward(s.rows) {
		if r.AccountID == accountID {
			mine = append(mine, r)
		}
	}

	from := min(offset, len(mine))

	return mine[from:min(from+limit, len(mine))], len(mine), nil
}

// LastSignIns implements account.SignInHistory.
func (s *SignIns) LastSignIns(_ context.Context, ids []int) (map[int]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := map[int]time.Time{}

	for _, r := range s.rows {
		if r.Event == account.SignedIn && slices.Contains(ids, r.AccountID) {
			out[r.AccountID] = r.CreatedAt
		}
	}

	return out, nil
}

// WrongPasswordsSince implements account.SignInHistory.
func (s *SignIns) WrongPasswordsSince(_ context.Context, since time.Time) ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ids []int

	for _, r := range s.rows {
		if r.Event == account.WrongPassword && r.CreatedAt.After(since) && !slices.Contains(ids, r.AccountID) {
			ids = append(ids, r.AccountID)
		}
	}

	return ids, nil
}

// All returns every row recorded, oldest first, for a test to read.
func (s *SignIns) All() []account.SignInEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.rows)
}

type sessionRow struct {
	account.Session
	access, refresh string
	revoked         bool
}

// Sessions is a memory account.SessionStore.
type Sessions struct {
	mu   sync.Mutex
	rows []*sessionRow
}

// NewSessions returns an empty store.
func NewSessions() *Sessions { return &Sessions{} }

func (r *sessionRow) live(now time.Time) bool { return !r.revoked && now.Before(r.ExpiresAt) }

// Open implements account.SessionStore.
func (s *Sessions) Open(_ context.Context, n account.NewSession) (account.Credentials, error) {
	access, err := account.NewToken()
	if err != nil {
		return account.Credentials{}, err
	}

	refresh, err := account.NewToken()
	if err != nil {
		return account.Credentials{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.rows = append(s.rows, &sessionRow{
		Session: account.Session{
			ID: len(s.rows) + 1, AccountID: n.AccountID, ActorID: n.ActorID, Device: n.Device, IP: n.IP,
			LastUsedAt: n.At, ExpiresAt: n.ExpiresAt, CreatedAt: n.At,
		},
		access: access, refresh: refresh,
	})

	return account.Credentials{Access: access, Refresh: refresh, ExpiresAt: n.ExpiresAt}, nil
}

// Get implements account.SessionStore.
func (s *Sessions) Get(_ context.Context, accessToken string, now time.Time) (account.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.rows {
		if r.access == accessToken && r.live(now) {
			return r.Session, nil
		}
	}

	return account.Session{}, account.ErrSessionNotFound
}

// Touch implements account.SessionStore.
func (s *Sessions) Touch(_ context.Context, sessionID int, at time.Time, ip string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.rows {
		if r.ID == sessionID {
			r.LastUsedAt, r.IP = at, ip
		}
	}

	return nil
}

// Rotate implements account.SessionStore.
func (s *Sessions) Rotate(_ context.Context, rot account.Rotation) (account.Session, account.Credentials, error) {
	access, err := account.NewToken()
	if err != nil {
		return account.Session{}, account.Credentials{}, err
	}

	refresh, err := account.NewToken()
	if err != nil {
		return account.Session{}, account.Credentials{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.rows {
		if r.refresh != rot.Refresh || !r.live(rot.At) {
			continue
		}

		r.access, r.refresh = access, refresh
		r.ExpiresAt, r.LastUsedAt, r.IP = rot.ExpiresAt, rot.At, rot.IP

		if rot.Device != "" {
			r.Device = rot.Device
		}

		return r.Session, account.Credentials{Access: access, Refresh: refresh, ExpiresAt: rot.ExpiresAt}, nil
	}

	return account.Session{}, account.Credentials{}, account.ErrSessionNotFound
}

// Live implements account.SessionStore.
func (s *Sessions) Live(_ context.Context, accountID int, now time.Time) ([]account.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []account.Session

	for _, r := range s.rows {
		if r.AccountID == accountID && r.live(now) {
			out = append(out, r.Session)
		}
	}

	slices.SortStableFunc(out, func(a, b account.Session) int { return b.LastUsedAt.Compare(a.LastUsedAt) })

	return out, nil
}

// End implements account.SessionStore.
func (s *Sessions) End(_ context.Context, accountID int, ids []int, keep int, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0

	for _, r := range s.rows {
		if r.AccountID != accountID || !r.live(now) || r.ID == keep || (len(ids) > 0 && !slices.Contains(ids, r.ID)) {
			continue
		}

		r.revoked = true
		n++
	}

	return n, nil
}

// EndOpenedBy implements account.SessionStore.
func (s *Sessions) EndOpenedBy(_ context.Context, actorID int, now time.Time) ([]account.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []account.Session

	for _, r := range s.rows {
		if r.ActorID == actorID && r.live(now) {
			r.revoked = true

			out = append(out, r.Session)
		}
	}

	return out, nil
}

// Codes is a memory account.CodeStore.
type Codes struct {
	mu   sync.Mutex
	next int
	rows []account.Code
}

// NewCodes returns an empty store.
func NewCodes() *Codes { return &Codes{next: 1} }

// Latest implements account.CodeStore.
func (s *Codes) Latest(_ context.Context, accountID int, p account.Purpose) (account.Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := len(s.rows) - 1; i >= 0; i-- {
		if c := s.rows[i]; c.AccountID == accountID && c.Purpose == p {
			return c, nil
		}
	}

	return account.Code{}, account.ErrCodeNotFound
}

// IssuedSince implements account.CodeStore.
func (s *Codes) IssuedSince(_ context.Context, accountID int, p account.Purpose, since time.Time) (int, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, oldest := 0, time.Time{}

	for _, c := range s.rows {
		if c.AccountID != accountID || c.Purpose != p || !c.CreatedAt.After(since) {
			continue
		}

		if n == 0 || c.CreatedAt.Before(oldest) {
			oldest = c.CreatedAt
		}

		n++
	}

	return n, oldest, nil
}

// Issue implements account.CodeStore.
func (s *Codes) Issue(_ context.Context, c account.Code, now time.Time) (account.Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.endLocked(c.AccountID, c.Purpose, now)

	c.ID = s.next
	s.next++
	s.rows = append(s.rows, c)

	return c, nil
}

func (s *Codes) endLocked(accountID int, p account.Purpose, now time.Time) {
	for i := range s.rows {
		if c := &s.rows[i]; c.AccountID == accountID && c.Purpose == p && c.ConsumedAt == nil && c.InvalidatedAt == nil {
			at := now
			c.InvalidatedAt = &at
		}
	}
}

// SaveVerification implements account.CodeStore.
func (s *Codes) SaveVerification(_ context.Context, c account.Code, readAttempts int, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.rows {
		if s.rows[i].ID != c.ID {
			continue
		}

		if s.rows[i].ConsumedAt != nil || s.rows[i].InvalidatedAt != nil || s.rows[i].Attempts != readAttempts {
			return account.ErrCodeConflict
		}

		s.rows[i].Attempts, s.rows[i].ConsumedAt, s.rows[i].InvalidatedAt = c.Attempts, c.ConsumedAt, c.InvalidatedAt

		return nil
	}

	return account.ErrCodeConflict
}

// InvalidateLive implements account.CodeStore.
func (s *Codes) InvalidateLive(_ context.Context, accountID int, p account.Purpose, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.endLocked(accountID, p, now)

	return nil
}

// Reauth is a memory account.ReauthStore.
type Reauth struct {
	mu   sync.Mutex
	rows map[int]account.Attempts
}

// NewReauth returns an empty store.
func NewReauth() *Reauth { return &Reauth{rows: map[int]account.Attempts{}} }

// Find implements account.ReauthStore.
func (s *Reauth) Find(_ context.Context, accountID int) (account.Attempts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.rows[accountID]
	if !ok {
		return account.Attempts{}, account.ErrReauthNotFound
	}

	return a, nil
}

// Create implements account.ReauthStore.
func (s *Reauth) Create(_ context.Context, a account.Attempts) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.rows[a.AccountID]; ok {
		return account.ErrReauthConflict
	}

	s.rows[a.AccountID] = a

	return nil
}

// Save implements account.ReauthStore.
func (s *Reauth) Save(_ context.Context, a, read account.Attempts) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.rows[read.AccountID]
	if !ok || cur.Failures != read.Failures || !sameTime(cur.LockedUntil, read.LockedUntil) {
		return account.ErrReauthConflict
	}

	s.rows[a.AccountID] = a

	return nil
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Equal(*b)
}

// ChangeLog is a memory account.ChangeLog.
type ChangeLog struct {
	mu    sync.Mutex
	clock account.Clock
	rows  []account.ChangeEntry
}

// NewChangeLog returns an empty log that stamps changes with the clock.
func NewChangeLog(clock account.Clock) *ChangeLog { return &ChangeLog{clock: clock} }

// Record implements account.ChangeLog.
func (l *ChangeLog) Record(_ context.Context, c account.Change) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.rows = append(l.rows, account.ChangeEntry{ID: len(l.rows) + 1, Change: c, At: l.clock.Now()})

	return nil
}

// Changes implements account.ChangeLog.
func (l *ChangeLog) Changes(_ context.Context, accountID, offset, limit int) ([]account.ChangeEntry, int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	var mine []account.ChangeEntry

	for _, r := range slices.Backward(l.rows) {
		if r.AccountID == accountID {
			mine = append(mine, r)
		}
	}

	from := min(offset, len(mine))

	return mine[from:min(from+limit, len(mine))], len(mine), nil
}

// Transactor is an account.Transactor over memory: it just runs the function.
// Memory stores cannot roll back, so a test of a veto checks that nothing was
// written before the refusal, which is what the services guarantee by asking
// the hooks first.
type Transactor struct{}

// WithinTransaction implements account.Transactor.
func (Transactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// ActivityLog is a memory account.ActivityLog over a ChangeLog and the Sessions:
// what a person did is the changes they made to accounts (record type "account"),
// and who was signed in as them is read off the sessions opened by signing in as them.
type ActivityLog struct {
	changes  *ChangeLog
	sessions *Sessions
}

// NewActivityLog returns the log over the stores a test already writes to.
func NewActivityLog(changes *ChangeLog, sessions *Sessions) *ActivityLog {
	return &ActivityLog{changes: changes, sessions: sessions}
}

// Activity implements account.ActivityLog.
func (l *ActivityLog) Activity(_ context.Context, q account.ActivityQuery) ([]account.ActivityEntry, int, error) {
	const recordType = "account"

	if (!q.Within.Empty() && !q.Within.Has(recordType)) || q.Outside.Has(recordType) {
		return nil, 0, nil
	}

	l.sessions.mu.Lock()

	var windows []account.Session

	for _, r := range l.sessions.rows {
		if r.AccountID == q.ActorID && r.ActorID > 0 {
			windows = append(windows, r.Session)
		}
	}

	l.sessions.mu.Unlock()

	l.changes.mu.Lock()
	defer l.changes.mu.Unlock()

	var all []account.ActivityEntry

	for _, r := range slices.Backward(l.changes.rows) {
		if r.ActorID != q.ActorID || (!q.From.IsZero() && r.At.Before(q.From)) || (!q.To.IsZero() && !r.At.Before(q.To)) {
			continue
		}

		entry := account.ActivityEntry{ID: r.ID, RecordType: recordType, RecordID: r.AccountID, Action: account.ActivityActionOf(string(r.Action)), At: r.At}

		for _, w := range slices.Backward(windows) {
			end := minTime(w.ExpiresAt, maxTime(w.LastUsedAt, w.CreatedAt).Add(time.Minute))
			if !r.At.Before(w.CreatedAt) && !r.At.After(end) {
				entry.SignedInAs = w.ActorID

				break
			}
		}

		all = append(all, entry)
	}

	from := min(q.Offset, len(all))

	return all[from:min(from+q.Limit, len(all))], len(all), nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
}
