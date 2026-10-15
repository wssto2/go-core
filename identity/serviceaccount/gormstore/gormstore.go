// Package gormstore is the SQL store of service accounts: GORM over the tables of migrations, MariaDB 10.3
// and MySQL safe. It passes storetest.
package gormstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/identity/serviceaccount"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The models mirror the migration files column for column (schema_test.go holds them equal).

type accountModel struct {
	ID            int        `gorm:"primaryKey;autoIncrement"`
	Name          string     `gorm:"size:100;not null"`
	Description   string     `gorm:"size:500;not null;default:''"`
	TenantID      *int       `gorm:"index:idx_service_accounts_tenant"`
	OwnerID       int        `gorm:"not null;index:idx_service_accounts_owner"`
	CreatedBy     int        `gorm:"not null"`
	CreatedAt     time.Time  `gorm:"type:datetime;not null;autoCreateTime:false"`
	UpdatedBy     int        `gorm:"not null;default:0"`
	UpdatedAt     *time.Time `gorm:"type:datetime;autoUpdateTime:false"`
	DeactivatedAt *time.Time `gorm:"type:datetime"`
	DeactivatedBy int        `gorm:"not null;default:0"`
}

func (accountModel) TableName() string { return "service_accounts" }

type keyModel struct {
	ID               int        `gorm:"primaryKey;autoIncrement"`
	ServiceAccountID int        `gorm:"not null;index:idx_service_account_keys_account"`
	Name             string     `gorm:"size:100;not null"`
	Prefix           string     `gorm:"size:16;not null"`
	KeyHash          string     `gorm:"type:char(64);not null;uniqueIndex:uq_service_account_keys_hash"`
	CreatedBy        int        `gorm:"not null"`
	CreatedAt        time.Time  `gorm:"type:datetime;not null;autoCreateTime:false"`
	LastUsedAt       *time.Time `gorm:"type:datetime"`
	ExpiresAt        *time.Time `gorm:"type:datetime"`
	RevokedAt        *time.Time `gorm:"type:datetime"`
	RevokedBy        int        `gorm:"not null;default:0"`
}

func (keyModel) TableName() string { return "service_account_keys" }

type noticeModel struct {
	ID     int       `gorm:"primaryKey;autoIncrement"`
	KeyID  int       `gorm:"not null;uniqueIndex:uq_service_account_key_notices,priority:1"`
	Kind   string    `gorm:"size:20;not null;uniqueIndex:uq_service_account_key_notices,priority:2"`
	SentAt time.Time `gorm:"type:datetime;not null"`
}

func (noticeModel) TableName() string { return "service_account_key_notices" }

// Migrate creates the tables from the models, for tests on SQLite; production runs the files in migrations.
func Migrate(db *gorm.DB) error {
	err := db.AutoMigrate(new(accountModel), new(keyModel), new(noticeModel))
	if err != nil {
		return fmt.Errorf("service accounts: migrate: %w", err)
	}

	return nil
}

// Store is the SQL store of service accounts.
type Store struct{ db *gorm.DB }

var _ serviceaccount.Store = (*Store)(nil)

// New returns the store over db.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// whole is a time as the DATETIME columns keep it: UTC, to the second.
func whole(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

func wholePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	w := whole(*t)

	return &w
}

func (m accountModel) account() serviceaccount.Account {
	tenant := 0
	if m.TenantID != nil {
		tenant = *m.TenantID
	}

	return serviceaccount.Account{
		ID: m.ID, Name: m.Name, Description: m.Description, TenantID: tenant, OwnerID: m.OwnerID,
		CreatedBy: m.CreatedBy, CreatedAt: m.CreatedAt.UTC(), UpdatedBy: m.UpdatedBy, UpdatedAt: utcPtr(m.UpdatedAt),
		DeactivatedAt: utcPtr(m.DeactivatedAt), DeactivatedBy: m.DeactivatedBy,
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	u := t.UTC()

	return &u
}

func modelOf(acc serviceaccount.Account) accountModel {
	var tenantID *int

	if acc.TenantID > 0 {
		tenant := acc.TenantID
		tenantID = &tenant
	}

	return accountModel{
		ID: acc.ID, Name: acc.Name, Description: acc.Description, OwnerID: acc.OwnerID, CreatedBy: acc.CreatedBy,
		CreatedAt: whole(acc.CreatedAt), UpdatedBy: acc.UpdatedBy, UpdatedAt: wholePtr(acc.UpdatedAt),
		DeactivatedAt: wholePtr(acc.DeactivatedAt), DeactivatedBy: acc.DeactivatedBy, TenantID: tenantID,
	}
}

func (m keyModel) key() serviceaccount.Key {
	return serviceaccount.Key{
		ID: m.ID, ServiceAccountID: m.ServiceAccountID, Name: m.Name, Prefix: m.Prefix, Hash: "", CreatedBy: m.CreatedBy,
		CreatedAt: m.CreatedAt.UTC(), LastUsedAt: utcPtr(m.LastUsedAt), ExpiresAt: utcPtr(m.ExpiresAt),
		RevokedAt: utcPtr(m.RevokedAt), RevokedBy: m.RevokedBy,
	}
}

// List implements serviceaccount.Store.
func (s *Store) List(
	ctx context.Context, visible serviceaccount.Visibility, when time.Time,
) ([]serviceaccount.Summary, error) {
	query := s.conn(ctx).Model(new(accountModel)).Order("name ASC, id ASC")

	switch {
	case visible.All:
	case len(visible.Tenants) > 0:
		query = query.Where("tenant_id IN ?", visible.Tenants)
	default:
		return []serviceaccount.Summary{}, nil
	}

	var rows []accountModel

	err := query.Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list service accounts: %w", err)
	}

	return s.summaries(ctx, rows, when)
}

// Find implements serviceaccount.Store.
func (s *Store) Find(ctx context.Context, accountID int, when time.Time) (serviceaccount.Summary, error) {
	var row accountModel

	err := s.conn(ctx).Where("id = ?", accountID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return serviceaccount.Summary{}, serviceaccount.ErrNotFound
	}

	if err != nil {
		return serviceaccount.Summary{}, fmt.Errorf("read service account %d: %w", accountID, err)
	}

	out, err := s.summaries(ctx, []accountModel{row}, when)
	if err != nil {
		return serviceaccount.Summary{}, err
	}

	return out[0], nil
}

// Create implements serviceaccount.Store.
func (s *Store) Create(ctx context.Context, account *serviceaccount.Account) error {
	row := modelOf(*account)
	row.ID = 0

	err := s.conn(ctx).Create(&row).Error
	if err != nil {
		return fmt.Errorf("create service account: %w", err)
	}

	account.ID = row.ID

	return nil
}

// Update implements serviceaccount.Store.
func (s *Store) Update(ctx context.Context, account serviceaccount.Account) error {
	row := modelOf(account)

	err := s.conn(ctx).Model(new(accountModel)).Where("id = ?", account.ID).
		Select("name", "description", "tenant_id", "owner_id", "updated_by", "updated_at", "deactivated_at",
			"deactivated_by").
		Updates(&row).Error
	if err != nil {
		return fmt.Errorf("update service account %d: %w", account.ID, err)
	}

	return nil
}

// Keys implements serviceaccount.Store.
func (s *Store) Keys(ctx context.Context, accountID int) ([]serviceaccount.Key, error) {
	var rows []keyModel

	err := s.conn(ctx).Where("service_account_id = ?", accountID).Order("id DESC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list keys of service account %d: %w", accountID, err)
	}

	out := make([]serviceaccount.Key, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.key())
	}

	return out, nil
}

// FindKey implements serviceaccount.Store.
func (s *Store) FindKey(ctx context.Context, accountID, keyID int) (serviceaccount.Key, error) {
	var row keyModel

	err := s.conn(ctx).Where("service_account_id = ? AND id = ?", accountID, keyID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return serviceaccount.Key{}, serviceaccount.ErrKeyNotFound
	}

	if err != nil {
		return serviceaccount.Key{}, fmt.Errorf("read key %d: %w", keyID, err)
	}

	return row.key(), nil
}

// CreateKey implements serviceaccount.Store.
func (s *Store) CreateKey(ctx context.Context, key *serviceaccount.Key) error {
	row := keyModel{
		ID: 0, ServiceAccountID: key.ServiceAccountID, Name: key.Name, Prefix: key.Prefix, KeyHash: key.Hash,
		CreatedBy: key.CreatedBy, CreatedAt: whole(key.CreatedAt), LastUsedAt: nil, ExpiresAt: wholePtr(key.ExpiresAt),
		RevokedAt: nil, RevokedBy: 0,
	}

	err := s.conn(ctx).Create(&row).Error
	if err != nil {
		return fmt.Errorf("create key: %w", err)
	}

	key.ID = row.ID

	return nil
}

// RevokeKey implements serviceaccount.Store.
func (s *Store) RevokeKey(ctx context.Context, keyID int, at time.Time, by int) error {
	err := s.conn(ctx).Model(new(keyModel)).Where("id = ? AND revoked_at IS NULL", keyID).
		Updates(map[string]any{"revoked_at": whole(at), "revoked_by": by}).Error
	if err != nil {
		return fmt.Errorf("revoke key %d: %w", keyID, err)
	}

	return nil
}

// FindByKeyHash implements serviceaccount.Store.
func (s *Store) FindByKeyHash(ctx context.Context, hash string) (serviceaccount.Key, serviceaccount.Account, error) {
	var key keyModel

	err := s.conn(ctx).Where("key_hash = ?", hash).Take(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return serviceaccount.Key{}, serviceaccount.Account{}, serviceaccount.ErrKeyNotFound
	}

	if err != nil {
		return serviceaccount.Key{}, serviceaccount.Account{}, fmt.Errorf("read key by hash: %w", err)
	}

	var account accountModel

	err = s.conn(ctx).Where("id = ?", key.ServiceAccountID).Take(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return serviceaccount.Key{}, serviceaccount.Account{}, serviceaccount.ErrKeyNotFound
	}

	if err != nil {
		return serviceaccount.Key{}, serviceaccount.Account{}, fmt.Errorf("read service account of key: %w", err)
	}

	return key.key(), account.account(), nil
}

// TouchKey implements serviceaccount.Store.
func (s *Store) TouchKey(ctx context.Context, keyID int, at time.Time) error {
	err := s.conn(ctx).Model(new(keyModel)).Where("id = ?", keyID).Update("last_used_at", whole(at)).Error
	if err != nil {
		return fmt.Errorf("touch key %d: %w", keyID, err)
	}

	return nil
}

// WorkingKeys implements serviceaccount.NoticeStore.
func (s *Store) WorkingKeys(ctx context.Context, at time.Time) ([]serviceaccount.KeyWithAccount, error) {
	var keys []keyModel

	err := s.conn(ctx).
		Where("revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", whole(at)).
		Where("service_account_id IN (?)", s.conn(ctx).Model(new(accountModel)).Select("id").Where("deactivated_at IS NULL")).
		Order("id ASC").Find(&keys).Error
	if err != nil {
		return nil, fmt.Errorf("read working keys: %w", err)
	}

	if len(keys) == 0 {
		return []serviceaccount.KeyWithAccount{}, nil
	}

	accountIDs := make([]int, 0, len(keys))
	for _, key := range keys {
		accountIDs = append(accountIDs, key.ServiceAccountID)
	}

	var accounts []accountModel

	err = s.conn(ctx).Where("id IN ?", accountIDs).Find(&accounts).Error
	if err != nil {
		return nil, fmt.Errorf("read accounts of working keys: %w", err)
	}

	byID := make(map[int]serviceaccount.Account, len(accounts))
	for _, row := range accounts {
		byID[row.ID] = row.account()
	}

	out := make([]serviceaccount.KeyWithAccount, 0, len(keys))

	for _, row := range keys {
		if account, ok := byID[row.ServiceAccountID]; ok {
			out = append(out, serviceaccount.KeyWithAccount{Key: row.key(), Account: account})
		}
	}

	return out, nil
}

// NoticesSent implements serviceaccount.NoticeStore.
func (s *Store) NoticesSent(ctx context.Context, keyIDs []int) (map[int][]serviceaccount.NoticeKind, error) {
	out := map[int][]serviceaccount.NoticeKind{}
	if len(keyIDs) == 0 {
		return out, nil
	}

	var rows []noticeModel

	err := s.conn(ctx).Where("key_id IN ?", keyIDs).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("read sent key notices: %w", err)
	}

	for _, row := range rows {
		out[row.KeyID] = append(out[row.KeyID], serviceaccount.NoticeKind(row.Kind))
	}

	return out, nil
}

// ClaimNotice implements serviceaccount.NoticeStore.
func (s *Store) ClaimNotice(
	ctx context.Context, keyID int, kind serviceaccount.NoticeKind, when time.Time,
) (bool, error) {
	claim := noticeModel{ID: 0, KeyID: keyID, Kind: string(kind), SentAt: whole(when)}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	ignoreDuplicate := clause.OnConflict{DoNothing: true}

	res := s.conn(ctx).Clauses(ignoreDuplicate).Create(&claim)
	if res.Error != nil {
		return false, fmt.Errorf("claim %s notice of key %d: %w", kind, keyID, res.Error)
	}

	return res.RowsAffected == 1, nil
}

// conn is the transaction in ctx (database.Transactor puts it there), else the store's own connection, so an
// account's change and its audit row commit together.
func (s *Store) conn(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx
	}

	return s.db.WithContext(ctx)
}

// summaries adds the key counts and last use to the accounts.
func (s *Store) summaries(
	ctx context.Context, rows []accountModel, when time.Time,
) ([]serviceaccount.Summary, error) {
	out := make([]serviceaccount.Summary, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}

	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	var active []struct {
		ServiceAccountID int
		ActiveKeys       int
	}

	// The alias is active_keys: KEYS is a reserved word in MySQL and MariaDB.
	err := s.conn(ctx).Model(new(keyModel)).
		Select("service_account_id, COUNT(*) AS active_keys").
		Where("service_account_id IN ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", ids, whole(when)).
		Group("service_account_id").Scan(&active).Error
	if err != nil {
		return nil, fmt.Errorf("count active keys: %w", err)
	}

	// The last use is read per key and folded here: MAX() of a DATETIME comes back as text on some drivers.
	var keys []keyModel

	err = s.conn(ctx).Select("service_account_id, last_used_at").
		Where("service_account_id IN ? AND last_used_at IS NOT NULL", ids).Find(&keys).Error
	if err != nil {
		return nil, fmt.Errorf("read last use of keys: %w", err)
	}

	counts := map[int]int{}
	for _, count := range active {
		counts[count.ServiceAccountID] = count.ActiveKeys
	}

	lastUsed := map[int]*time.Time{}

	for _, used := range keys {
		current := lastUsed[used.ServiceAccountID]
		if current == nil || used.LastUsedAt.After(*current) {
			lastUsed[used.ServiceAccountID] = utcPtr(used.LastUsedAt)
		}
	}

	for _, row := range rows {
		out = append(out, serviceaccount.Summary{
			Account: row.account(), ActiveKeys: counts[row.ID], LastUsedAt: lastUsed[row.ID],
		})
	}

	return out, nil
}
