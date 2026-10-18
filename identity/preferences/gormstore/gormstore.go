// Package gormstore is the SQL store of preferences: GORM over the table of migrations, MariaDB 10.3 and MySQL
// safe. It passes storetest.
package gormstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/identity/preferences"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// model mirrors the migration file (schema_test.go holds them equal).
type model struct {
	AccountID   int    `gorm:"primaryKey;autoIncrement:false"`
	Preferences string `gorm:"type:longtext;not null"`
}

func (model) TableName() string { return "account_preferences" }

// Migrate creates the table from the model, for tests on SQLite; production runs the file in migrations.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(new(model)); err != nil {
		return fmt.Errorf("preferences: migrate: %w", err)
	}

	return nil
}

// Store is the SQL store of preferences.
type Store struct{ db *gorm.DB }

var _ preferences.Store = (*Store)(nil)

// New returns the store over db.
func New(db *gorm.DB) *Store { return &Store{db: db} }

func (s *Store) on(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx
	}

	return s.db.WithContext(ctx)
}

// Get implements preferences.Store.
func (s *Store) Get(ctx context.Context, accountID int) (json.RawMessage, error) {
	var m model

	err := s.on(ctx).Where("account_id = ?", accountID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // nothing stored is an empty object, not an error
	}

	if err != nil {
		return nil, fmt.Errorf("preferences: read: %w", err)
	}

	return json.RawMessage(m.Preferences), nil
}

// Set implements preferences.Store: an upsert on the account.
func (s *Store) Set(ctx context.Context, accountID int, prefs json.RawMessage) error {
	err := s.on(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "account_id"}}, DoUpdates: clause.AssignmentColumns([]string{"preferences"}),
	}).Create(&model{AccountID: accountID, Preferences: string(prefs)}).Error
	if err != nil {
		return fmt.Errorf("preferences: write: %w", err)
	}

	return nil
}
