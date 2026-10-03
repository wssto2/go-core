package identitytest

import (
	"context"
	"slices"
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

	if a.ID == 0 {
		a.ID = s.next
	}

	s.next = max(s.next, a.ID) + 1
	s.rows[a.ID] = a

	return a, nil
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
