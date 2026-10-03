// Package gormstore is identity's default store, on GORM: accounts, the
// sign-in history and sessions in the tables identity/migrations creates. It
// is safe for MariaDB 10.3: its only locking read is a single-table
// SELECT ... FOR UPDATE.
//
//	stores := gormstore.New(app.Database())
//	svc, err := account.New(account.Deps{Accounts: stores.Accounts, SignIns: stores.SignIns, Sessions: stores.Sessions, Clock: app.Clock()}, account.Config{})
package gormstore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/identity/account"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Accounts implements account.AccountStore, SignIns account.SignInLog and
// Sessions account.SessionStore.
type (
	Accounts struct{ db *gorm.DB }
	SignIns  struct{ db *gorm.DB }
	Sessions struct {
		db      *gorm.DB
		refresh auth.Hasher
	}
)

var (
	_ account.AccountStore = (*Accounts)(nil)
	_ account.SignInLog    = (*SignIns)(nil)
	_ account.SessionStore = (*Sessions)(nil)
)

// Stores are the three stores over one database.
type Stores struct {
	Accounts *Accounts
	SignIns  *SignIns
	Sessions *Sessions
}

// Option adjusts New.
type Option func(*Sessions)

// WithRefreshHasher sets how refresh tokens are hashed at rest. The default is
// SHA-256, right for a random 256-bit token; use it to read sessions an
// application already stored with another hasher (auth.HMACHasher, auth.BcryptHasher).
func WithRefreshHasher(h auth.Hasher) Option { return func(s *Sessions) { s.refresh = h } }

// New returns the stores over db.
func New(db *gorm.DB, opts ...Option) Stores {
	sessions := &Sessions{db: db, refresh: sha256Hasher{}}
	for _, opt := range opts {
		opt(sessions)
	}

	return Stores{Accounts: &Accounts{db: db}, SignIns: &SignIns{db: db}, Sessions: sessions}
}

// sha256Hasher hashes a high-entropy token: a slow hash buys nothing there.
type sha256Hasher struct{}

func (sha256Hasher) Hash(token string) (string, error) {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:]), nil
}

func (h sha256Hasher) Compare(token, hash string) bool {
	want, _ := h.Hash(token)

	return subtle.ConstantTimeCompare([]byte(want), []byte(hash)) == 1
}

// whole is a time as the DATETIME columns keep it: UTC, to the second.
func whole(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

// millis is a time as the datetime(3) columns keep it.
func millis(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }

// --- accounts ---

func (m accountModel) account() account.Account {
	return account.Account{
		ID: m.ID, Login: m.Login, Email: m.Email, Name: m.Name, Locale: m.Locale, Active: m.Active,
		PasswordHash: m.PasswordHash, CreatedAt: m.CreatedAt.UTC(),
	}
}

// Find implements account.AccountStore.
func (s *Accounts) Find(ctx context.Context, id int) (account.Account, error) {
	return s.one(ctx, "id = ?", id)
}

// FindByLogin implements account.AccountStore.
func (s *Accounts) FindByLogin(ctx context.Context, login string) (account.Account, error) {
	return s.one(ctx, "login = ?", account.NormalizeLogin(login))
}

func (s *Accounts) one(ctx context.Context, where string, arg any) (account.Account, error) {
	var m accountModel

	err := s.db.WithContext(ctx).Where(where, arg).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return account.Account{}, account.ErrNotFound
	}

	if err != nil {
		return account.Account{}, err
	}

	return m.account(), nil
}

// Create implements account.AccountStore.
func (s *Accounts) Create(ctx context.Context, a account.Account) (account.Account, error) {
	m := accountModel{
		Login: account.NormalizeLogin(a.Login), Email: a.Email, Name: a.Name, PasswordHash: a.PasswordHash,
		Locale: a.Locale, Active: a.Active, CreatedAt: whole(a.CreatedAt), UpdatedAt: whole(a.CreatedAt),
	}

	if a.ID > 0 {
		m.ID = a.ID
	}

	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		// A login in use is the one failure a caller can act on; the driver's error
		// for it differs, so ask the table.
		var n int64
		if s.db.WithContext(ctx).Model(&accountModel{}).Where("login = ?", m.Login).Count(&n).Error == nil && n > 0 {
			return account.Account{}, account.ErrLoginTaken
		}

		return account.Account{}, err
	}

	return m.account(), nil
}

func (s *Accounts) set(ctx context.Context, id int, column string, value any) error {
	tx := s.db.WithContext(ctx).Model(&accountModel{}).Where("id = ?", id).Updates(map[string]any{column: value, "updated_at": time.Now().UTC().Truncate(time.Second)})
	if tx.Error != nil {
		return tx.Error
	}

	if tx.RowsAffected == 0 { // MySQL counts changed rows: an unchanged value is not a missing account
		if _, err := s.Find(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

// SetLocale implements account.AccountStore.
func (s *Accounts) SetLocale(ctx context.Context, id int, locale string) error {
	return s.set(ctx, id, "locale", locale)
}

// SetPasswordHash implements account.AccountStore.
func (s *Accounts) SetPasswordHash(ctx context.Context, id int, hash string) error {
	return s.set(ctx, id, "password_hash", hash)
}

// SetActive implements account.AccountStore.
func (s *Accounts) SetActive(ctx context.Context, id int, active bool) error {
	return s.set(ctx, id, "active", active)
}

// --- sign-in history ---

// Record implements account.SignInLog.
func (s *SignIns) Record(ctx context.Context, row account.SignInEntry) error {
	m := signInModel{
		UserID: row.AccountID, Event: string(row.Event), IP: row.IP, UserAgent: row.Device, CreatedAt: whole(row.CreatedAt),
	}

	if row.ActorID > 0 {
		m.ActorID = &row.ActorID
	}

	return s.db.WithContext(ctx).Create(&m).Error
}

// LockEvents implements account.SignInLog.
func (s *SignIns) LockEvents(ctx context.Context, accountID, n int) ([]account.SignInEntry, error) {
	var rows []signInModel

	err := s.db.WithContext(ctx).
		Where("user_id = ? AND event IN ?", accountID, []string{string(account.WrongPassword), string(account.SignedIn), string(account.Unlocked)}).
		Order("id DESC").Limit(n).Find(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]account.SignInEntry, len(rows))
	for i, r := range rows {
		out[i] = account.SignInEntry{
			ID: r.ID, AccountID: r.UserID, Event: account.Event(r.Event), IP: r.IP, Device: r.UserAgent, CreatedAt: r.CreatedAt.UTC(),
		}
		if r.ActorID != nil {
			out[i].ActorID = *r.ActorID
		}
	}

	return out, nil
}

// --- sessions ---

// A session opened by signing in as somebody else is named
// "login-as:<actor id>|<device>" in tokens.name, as arv-next names them.
const (
	impersonationPrefix = "login-as:"
	maxDevice           = 200 // keeps the whole name within the column
)

func nameOf(actorID int, device string) string {
	if actorID <= 0 {
		return device
	}

	if r := []rune(device); len(r) > maxDevice {
		device = string(r[:maxDevice])
	}

	return impersonationPrefix + strconv.Itoa(actorID) + "|" + device
}

func partsOf(name string) (actorID int, device string) {
	rest, ok := strings.CutPrefix(name, impersonationPrefix)
	if !ok {
		return 0, name
	}

	id, device, _ := strings.Cut(rest, "|")

	actor, err := strconv.Atoi(id)
	if err != nil || actor <= 0 {
		return 0, name
	}

	return actor, device
}

func sessionOf(t auth.Token) account.Session {
	actor, device := partsOf(t.Name)

	return account.Session{
		ID: t.ID, AccountID: t.UserID, ActorID: actor, Device: device, IP: t.LastUsedIP,
		LastUsedAt: t.LastUsedAt.UTC(), ExpiresAt: t.ExpiresAt.UTC(), CreatedAt: t.CreatedAt.UTC(),
	}
}

// Open implements account.SessionStore.
func (s *Sessions) Open(ctx context.Context, n account.NewSession) (account.Credentials, error) {
	access, err := account.NewToken()
	if err != nil {
		return account.Credentials{}, err
	}

	refresh, err := account.NewToken()
	if err != nil {
		return account.Credentials{}, err
	}

	hash, err := s.refresh.Hash(refresh)
	if err != nil {
		return account.Credentials{}, err
	}

	t := auth.Token{
		UserID: n.AccountID, TokenValue: access, Name: nameOf(n.ActorID, n.Device),
		LastUsedAt: millis(n.At), LastUsedIP: n.IP, ExpiresAt: millis(n.ExpiresAt), CreatedAt: millis(n.At),
		RefreshPrefix: refresh[:8], RefreshToken: hash,
	}

	if err := s.db.WithContext(ctx).Create(&t).Error; err != nil {
		return account.Credentials{}, err
	}

	return account.Credentials{Access: access, Refresh: refresh, ExpiresAt: t.ExpiresAt}, nil
}

// live is the condition of a session that can be used at now.
func live(db *gorm.DB, now time.Time) *gorm.DB {
	return db.Where("revoked = ? AND expires_at > ?", false, millis(now))
}

// Get implements account.SessionStore.
func (s *Sessions) Get(ctx context.Context, accessToken string, now time.Time) (account.Session, error) {
	var t auth.Token

	err := live(s.db.WithContext(ctx), now).Where("token_value = ?", accessToken).Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return account.Session{}, account.ErrSessionNotFound
	}

	if err != nil {
		return account.Session{}, err
	}

	return sessionOf(t), nil
}

// Touch implements account.SessionStore.
func (s *Sessions) Touch(ctx context.Context, sessionID int, at time.Time, ip string) error {
	return s.db.WithContext(ctx).Model(&auth.Token{}).Where("id = ?", sessionID).
		Updates(map[string]any{"last_used_at": millis(at), "last_used_ip": ip}).Error
}

// Rotate implements account.SessionStore. The session is found by the token's
// prefix, then locked by its primary key (the only locking read MariaDB 10.3
// lets us rely on) inside one transaction.
func (s *Sessions) Rotate(ctx context.Context, r account.Rotation) (account.Session, account.Credentials, error) {
	if len(r.Refresh) < 8 {
		return account.Session{}, account.Credentials{}, account.ErrSessionNotFound
	}

	access, err := account.NewToken()
	if err != nil {
		return account.Session{}, account.Credentials{}, err
	}

	refresh, err := account.NewToken()
	if err != nil {
		return account.Session{}, account.Credentials{}, err
	}

	hash, err := s.refresh.Hash(refresh)
	if err != nil {
		return account.Session{}, account.Credentials{}, err
	}

	var (
		session account.Session
		creds   account.Credentials
	)

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The candidates are read without a lock: a locking read on the prefix index
		// takes gap locks that deadlock with the update of that very index.
		var candidates []auth.Token

		if err := live(tx, r.At).Where("refresh_prefix = ?", r.Refresh[:8]).Find(&candidates).Error; err != nil {
			return err
		}

		var match *auth.Token

		// Every candidate is compared, whether or not an earlier one matched: the
		// time does not say which prefix exists.
		for i := range candidates {
			if s.refresh.Compare(r.Refresh, candidates[i].RefreshToken) && match == nil {
				match = &candidates[i]
			}
		}

		if match == nil {
			return account.ErrSessionNotFound
		}

		// Then the one row is locked by its primary key and checked again: of two
		// requests with one refresh token, the second waits here, and finds the
		// token already rotated.
		var found auth.Token

		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", match.ID).Take(&found).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return account.ErrSessionNotFound
		}

		if err != nil {
			return err
		}

		if found.Revoked || !found.ExpiresAt.After(millis(r.At)) || !s.refresh.Compare(r.Refresh, found.RefreshToken) {
			return account.ErrSessionNotFound
		}

		updates := map[string]any{
			"token_value": access, "refresh_token": hash, "refresh_prefix": refresh[:8],
			"expires_at": millis(r.ExpiresAt), "last_used_at": millis(r.At), "last_used_ip": r.IP,
		}

		if r.Device != "" {
			// a session opened by signing in as somebody else stays marked as one
			actor, _ := partsOf(found.Name)
			updates["name"] = nameOf(actor, r.Device)
		}

		if err := tx.Model(&auth.Token{}).Where("id = ?", found.ID).Updates(updates).Error; err != nil {
			return err
		}

		found.TokenValue, found.ExpiresAt, found.LastUsedAt, found.LastUsedIP = access, millis(r.ExpiresAt), millis(r.At), r.IP
		if name, ok := updates["name"].(string); ok {
			found.Name = name
		}

		session = sessionOf(found)
		creds = account.Credentials{Access: access, Refresh: refresh, ExpiresAt: found.ExpiresAt}

		return nil
	})
	if err != nil {
		return account.Session{}, account.Credentials{}, err
	}

	return session, creds, nil
}

// Live implements account.SessionStore.
func (s *Sessions) Live(ctx context.Context, accountID int, now time.Time) ([]account.Session, error) {
	var rows []auth.Token

	err := live(s.db.WithContext(ctx), now).Where("user_id = ?", accountID).Order("last_used_at DESC, id DESC").Find(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]account.Session, len(rows))
	for i, t := range rows {
		out[i] = sessionOf(t)
	}

	return out, nil
}

// End implements account.SessionStore.
func (s *Sessions) End(ctx context.Context, accountID int, ids []int, keep int, now time.Time) (int, error) {
	q := live(s.db.WithContext(ctx).Model(&auth.Token{}), now).Where("user_id = ?", accountID)

	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	}

	if keep > 0 {
		q = q.Where("id <> ?", keep)
	}

	tx := q.Update("revoked", true)

	return int(tx.RowsAffected), tx.Error
}

// EndOpenedBy implements account.SessionStore.
func (s *Sessions) EndOpenedBy(ctx context.Context, actorID int, now time.Time) ([]account.Session, error) {
	prefix := impersonationPrefix + strconv.Itoa(actorID)

	var out []account.Session

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []auth.Token

		err := live(tx, now).Where("name = ? OR name LIKE ?", prefix, prefix+"|%").Find(&rows).Error
		if err != nil || len(rows) == 0 {
			return err
		}

		ids := make([]int, len(rows))
		for i, t := range rows {
			ids[i] = t.ID
			out = append(out, sessionOf(t))
		}

		return tx.Model(&auth.Token{}).Where("id IN ?", ids).Update("revoked", true).Error
	})
	if err != nil {
		return nil, fmt.Errorf("ending the sessions opened by %d: %w", actorID, err)
	}

	return out, nil
}
