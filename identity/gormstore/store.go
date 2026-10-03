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
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/identity/account"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Accounts implements account.Store.
type Accounts struct{ db *gorm.DB }

// SignIns implements account.SignInLog.
type SignIns struct{ db *gorm.DB }

// Sessions implements account.SessionStore.
type Sessions struct {
	db      *gorm.DB
	refresh auth.Hasher
}

var (
	_ account.Store         = (*Accounts)(nil)
	_ account.SignInLog     = (*SignIns)(nil)
	_ account.SessionStore  = (*Sessions)(nil)
	_ account.CodeStore     = (*Codes)(nil)
	_ account.Searcher      = (*Accounts)(nil)
	_ account.SignInHistory = (*SignIns)(nil)
	_ account.ReauthStore   = (*Reauth)(nil)
)

// Codes implements account.CodeStore.
type Codes struct{ db *gorm.DB }

// Reauth implements account.ReauthStore.
type Reauth struct{ db *gorm.DB }

// Stores are the stores over one database.
type Stores struct {
	Accounts *Accounts
	SignIns  *SignIns
	Sessions *Sessions
	Codes    *Codes
	Reauth   *Reauth
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

	return Stores{Accounts: &Accounts{db: db}, SignIns: &SignIns{db: db}, Sessions: sessions, Codes: &Codes{db: db}, Reauth: &Reauth{db: db}}
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

// dbOf is the database a store works on: the transaction the context carries
// (database.Transactor puts it there), else the store's own connection. This is
// what lets a hook's writes, an account's change and its history commit together.
func dbOf(db *gorm.DB, ctx context.Context) *gorm.DB { //nolint:revive // the receiver reads better first
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx
	}

	return db.WithContext(ctx)
}

// whole is a time as the DATETIME columns keep it: UTC, to the second.
func whole(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

// millis is a time as the datetime(3) columns keep it.
func millis(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }

// --- accounts ---

func (m accountModel) account() account.Account {
	return account.Account{
		ID: m.ID, Login: m.Login, Email: m.Email, Phone: m.Phone, Name: m.Name, Locale: m.Locale, Active: m.Active,
		PasswordHash: m.PasswordHash, CreatedAt: m.CreatedAt.UTC(),
	}
}

// Find implements account.Store.
func (s *Accounts) Find(ctx context.Context, id int) (account.Account, error) {
	return s.one(ctx, "id = ?", id)
}

// FindByLogin implements account.Store.
func (s *Accounts) FindByLogin(ctx context.Context, login string) (account.Account, error) {
	return s.one(ctx, "login = ?", account.NormalizeLogin(login))
}

// FindMany implements account.Store.
func (s *Accounts) FindMany(ctx context.Context, ids []int) ([]account.Account, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var rows []accountModel
	if err := dbOf(s.db, ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]account.Account, len(rows))
	for i, m := range rows {
		out[i] = m.account()
	}

	return out, nil
}

func (s *Accounts) one(ctx context.Context, where string, arg any) (account.Account, error) {
	var m accountModel

	err := dbOf(s.db, ctx).Where(where, arg).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return account.Account{}, account.ErrNotFound
	}

	if err != nil {
		return account.Account{}, err
	}

	return m.account(), nil
}

// Create implements account.Store.
func (s *Accounts) Create(ctx context.Context, a account.Account) (account.Account, error) {
	m := accountModel{
		Login: account.NormalizeLogin(a.Login), Email: a.Email, Phone: a.Phone, Name: a.Name, PasswordHash: a.PasswordHash,
		Locale: a.Locale, Active: a.Active, CreatedAt: whole(a.CreatedAt), UpdatedAt: whole(a.CreatedAt),
	}

	if a.ID > 0 {
		m.ID = a.ID
	}

	if err := dbOf(s.db, ctx).Create(&m).Error; err != nil {
		// A login in use is the one failure a caller can act on; the driver's error
		// for it differs, so ask the table.
		var n int64
		if dbOf(s.db, ctx).Model(&accountModel{}).Where("login = ?", m.Login).Count(&n).Error == nil && n > 0 {
			return account.Account{}, account.ErrLoginTaken
		}

		return account.Account{}, err
	}

	return m.account(), nil
}

func (s *Accounts) set(ctx context.Context, id int, column string, value any) error {
	tx := dbOf(s.db, ctx).Model(&accountModel{}).Where("id = ?", id).Updates(map[string]any{column: value, "updated_at": time.Now().UTC().Truncate(time.Second)})
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

// SetLocale implements account.Store.
func (s *Accounts) SetLocale(ctx context.Context, id int, locale string) error {
	return s.set(ctx, id, "locale", locale)
}

// SetPasswordHash implements account.Store.
func (s *Accounts) SetPasswordHash(ctx context.Context, id int, hash string) error {
	return s.set(ctx, id, "password_hash", hash)
}

// SetActive implements account.Store.
func (s *Accounts) SetActive(ctx context.Context, id int, active bool) error {
	return s.set(ctx, id, "active", active)
}

// FindByEmail implements account.Store: the address is compared without case.
func (s *Accounts) FindByEmail(ctx context.Context, email string) (account.Account, error) {
	return s.one(ctx, "LOWER(email) = ?", account.NormalizeEmail(email))
}

// Update implements account.Store: only the fields that are set are written.
func (s *Accounts) Update(ctx context.Context, id int, c account.Changes) error {
	set := map[string]any{}

	if c.Login != nil {
		set["login"] = account.NormalizeLogin(*c.Login)
	}

	if c.Email != nil {
		set["email"] = *c.Email
	}

	if c.Name != nil {
		set["name"] = *c.Name
	}

	if c.Phone != nil {
		set["phone"] = *c.Phone
	}

	if c.Locale != nil {
		set["locale"] = *c.Locale
	}

	if len(set) == 0 {
		_, err := s.Find(ctx, id)

		return err
	}

	set["updated_at"] = time.Now().UTC().Truncate(time.Second)

	tx := dbOf(s.db, ctx).Model(&accountModel{}).Where("id = ?", id).Updates(set)
	if tx.Error != nil {
		// A login in use is the one failure a caller can act on; the driver's error for it differs, so ask the table.
		if c.Login != nil {
			var n int64
			if dbOf(s.db, ctx).Model(&accountModel{}).Where("login = ? AND id <> ?", account.NormalizeLogin(*c.Login), id).Count(&n).Error == nil && n > 0 {
				return account.ErrLoginTaken
			}
		}

		return tx.Error
	}

	if tx.RowsAffected == 0 { // MySQL counts changed rows: an unchanged value is not a missing account
		if _, err := s.Find(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

// likePattern is term as a LIKE pattern that matches it anywhere, the wildcards in it escaped with "!".
func likePattern(term string) string {
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(term))

	return "%" + escaped + "%"
}

// Search implements account.Searcher.
func (s *Accounts) Search(ctx context.Context, q account.Query) (account.Page, error) {
	db := dbOf(s.db, ctx).Model(&accountModel{})

	if q.Search != "" {
		p := likePattern(q.Search)
		db = db.Where("LOWER(login) LIKE ? ESCAPE '!' OR LOWER(name) LIKE ? ESCAPE '!' OR LOWER(email) LIKE ? ESCAPE '!'", p, p, p)
	}

	if q.Active != nil {
		db = db.Where("active = ?", *q.Active)
	}

	switch {
	case q.Locked != nil && *q.Locked && len(q.LockedIDs) == 0:
		db = db.Where("1 = 0")
	case q.Locked != nil && *q.Locked:
		db = db.Where("id IN ?", q.LockedIDs)
	case q.Locked != nil && len(q.LockedIDs) > 0:
		db = db.Where("id NOT IN ?", q.LockedIDs)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return account.Page{}, err
	}

	column := map[account.Order]string{
		account.OrderLogin: "login", account.OrderName: "name", account.OrderEmail: "email", account.OrderCreated: "created_at",
	}[q.OrderBy]
	if column == "" {
		column = "login"
	}

	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}

	perPage := max(q.PerPage, 1)

	var rows []accountModel
	if err := db.Order(column + " " + dir + ", id " + dir).Offset((max(q.Page, 1) - 1) * perPage).Limit(perPage).Find(&rows).Error; err != nil {
		return account.Page{}, err
	}

	page := account.Page{Total: int(total), Accounts: make([]account.Account, len(rows))}
	for i, m := range rows {
		page.Accounts[i] = m.account()
	}

	return page, nil
}

// --- sign-in history ---

// Record implements account.SignInLog: one row of the history (IAM-USER-003).
func (s *SignIns) Record(ctx context.Context, row account.SignInEntry) error {
	m := signInModel{
		UserID: row.AccountID, Event: string(row.Event), IP: row.IP, UserAgent: row.Device, CreatedAt: whole(row.CreatedAt),
	}

	if row.ActorID > 0 {
		m.ActorID = &row.ActorID
	}

	return dbOf(s.db, ctx).Create(&m).Error
}

// LockEvents implements account.SignInLog.
func (s *SignIns) LockEvents(ctx context.Context, accountID, n int) ([]account.SignInEntry, error) {
	var rows []signInModel

	err := dbOf(s.db, ctx).
		Where("user_id = ? AND event IN ?", accountID, []string{string(account.WrongPassword), string(account.SignedIn), string(account.Unlocked)}).
		Order("id DESC").Limit(n).Find(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]account.SignInEntry, len(rows))
	for i, r := range rows {
		out[i] = r.entry()
	}

	return out, nil
}

// Entries implements account.SignInHistory.
func (s *SignIns) Entries(ctx context.Context, accountID, offset, limit int) ([]account.SignInEntry, int, error) {
	var total int64
	if err := dbOf(s.db, ctx).Model(&signInModel{}).Where("user_id = ?", accountID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []signInModel
	if err := dbOf(s.db, ctx).Where("user_id = ?", accountID).Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	out := make([]account.SignInEntry, len(rows))
	for i, r := range rows {
		out[i] = r.entry()
	}

	return out, int(total), nil
}

func (r signInModel) entry() account.SignInEntry {
	e := account.SignInEntry{ID: r.ID, AccountID: r.UserID, Event: account.Event(r.Event), IP: r.IP, Device: r.UserAgent, CreatedAt: r.CreatedAt.UTC()}
	if r.ActorID != nil {
		e.ActorID = *r.ActorID
	}

	return e
}

// LastSignIns implements account.SignInHistory.
func (s *SignIns) LastSignIns(ctx context.Context, ids []int) (map[int]time.Time, error) {
	out := map[int]time.Time{}
	if len(ids) == 0 {
		return out, nil
	}

	var latest []struct{ ID int }

	err := dbOf(s.db, ctx).Model(&signInModel{}).Select("MAX(id) AS id").
		Where("event = ? AND user_id IN ?", string(account.SignedIn), ids).Group("user_id").Scan(&latest).Error
	if err != nil || len(latest) == 0 {
		return out, err
	}

	rowIDs := make([]int, len(latest))
	for i, l := range latest {
		rowIDs[i] = l.ID
	}

	var rows []signInModel
	if err := dbOf(s.db, ctx).Where("id IN ?", rowIDs).Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, r := range rows {
		out[r.UserID] = r.CreatedAt.UTC()
	}

	return out, nil
}

// WrongPasswordsSince implements account.SignInHistory.
func (s *SignIns) WrongPasswordsSince(ctx context.Context, since time.Time) ([]int, error) {
	var ids []int

	err := dbOf(s.db, ctx).Model(&signInModel{}).Distinct().
		Where("event = ? AND created_at > ?", string(account.WrongPassword), whole(since)).Pluck("user_id", &ids).Error

	return ids, err
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

	if err := dbOf(s.db, ctx).Create(&t).Error; err != nil {
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

	err := live(dbOf(s.db, ctx), now).Where("token_value = ?", accessToken).Take(&t).Error
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
	return dbOf(s.db, ctx).Model(&auth.Token{}).Where("id = ?", sessionID).
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

	err = dbOf(s.db, ctx).Transaction(func(tx *gorm.DB) error {
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

	err := live(dbOf(s.db, ctx), now).Where("user_id = ?", accountID).Order("last_used_at DESC, id DESC").Find(&rows).Error
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
	q := live(dbOf(s.db, ctx).Model(&auth.Token{}), now).Where("user_id = ?", accountID)

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

	err := dbOf(s.db, ctx).Transaction(func(tx *gorm.DB) error {
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
