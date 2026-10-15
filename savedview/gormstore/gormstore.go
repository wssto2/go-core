// Package gormstore is the SQL store of the saved-view module: GORM over the table of savedview/migrations, MariaDB
// 10.3 and MySQL safe (no SKIP LOCKED, no FOR UPDATE OF, no FOR SHARE). It passes storetest.
package gormstore

import (
	"context"
	"errors"
	"time"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/savedview/core"
	"gorm.io/gorm"
)

// The columns the store names more than once.
const (
	whereOwner = "user_id = ? AND list_key = ?"
	whereRow   = "id = ? AND user_id = ?"
)

// model mirrors the migration file column for column (schema_test.go holds them equal). Its types are the least that
// lets GORM create the table on SQLite for tests, which cannot run the MySQL DDL.
type model struct {
	ID        int       `gorm:"primaryKey;autoIncrement"`
	UserID    int       `gorm:"not null;uniqueIndex:saved_views_user_list_name,priority:1;index:saved_views_user_list,priority:1"`          //nolint:lll // the index names are in the tag
	ListKey   string    `gorm:"size:100;not null;uniqueIndex:saved_views_user_list_name,priority:2;index:saved_views_user_list,priority:2"` //nolint:lll // the index names are in the tag
	Name      string    `gorm:"size:60;not null;uniqueIndex:saved_views_user_list_name,priority:3"`
	State     string    `gorm:"type:text;not null"`
	CreatedAt time.Time `gorm:"type:datetime;not null"`
	UpdatedAt time.Time `gorm:"type:datetime;not null;autoUpdateTime:false"`
}

func (model) TableName() string { return "saved_views" }

func (m model) view() core.View {
	return core.View{
		ID: m.ID, UserID: m.UserID, ListKey: m.ListKey, Name: m.Name, State: m.State,
		CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(),
	}
}

// Migrate creates the table from the model, for tests on SQLite; production runs the file in migrations.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(new(model)) //nolint:wrapcheck // the caller names the step
}

// Store is the SQL store of saved views.
type Store struct{ db *gorm.DB }

var _ core.Store = (*Store)(nil)

// New returns the store over db.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// List implements core.Store.
func (s *Store) List(ctx context.Context, userID int, listKey string) ([]core.View, error) {
	var rows []model

	err := s.conn(ctx).Where(whereOwner, userID, listKey).Order("name ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]core.View, len(rows))
	for i, row := range rows {
		out[i] = row.view()
	}

	return out, nil
}

// FindByName implements core.Store.
func (s *Store) FindByName(ctx context.Context, userID int, listKey, name string) (core.View, error) {
	var row model

	err := s.conn(ctx).Where(whereOwner+" AND name = ?", userID, listKey, name).First(&row).Error

	switch {
	case err == nil:
		return row.view(), nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return core.View{}, core.ErrNotFound
	default:
		return core.View{}, err
	}
}

// Count implements core.Store.
func (s *Store) Count(ctx context.Context, userID int, listKey string) (int, error) {
	var count int64

	err := s.conn(ctx).Model(new(model)).Where(whereOwner, userID, listKey).Count(&count).Error

	return int(count), err
}

// Save implements core.Store.
func (s *Store) Save(ctx context.Context, view *core.View) error {
	if view.ID == 0 {
		row := model{
			ID: 0, UserID: view.UserID, ListKey: view.ListKey, Name: view.Name, State: view.State,
			CreatedAt: view.CreatedAt.UTC(), UpdatedAt: view.UpdatedAt.UTC(),
		}

		err := s.conn(ctx).Create(&row).Error
		if err != nil {
			return err
		}

		view.ID = row.ID

		return nil
	}

	result := s.conn(ctx).Model(new(model)).Where(whereRow, view.ID, view.UserID).
		Updates(map[string]any{"state": view.State, "updated_at": view.UpdatedAt.UTC()})
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return s.requireRow(ctx, view.ID, view.UserID)
	}

	return nil
}

// Delete implements core.Store.
func (s *Store) Delete(ctx context.Context, userID, id int) error {
	result := s.conn(ctx).Where(whereRow, id, userID).Delete(new(model))
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return core.ErrNotFound
	}

	return nil
}

// requireRow is nil when the person's row exists, else ErrNotFound: MySQL reports 0 rows for an update that wrote what
// was already there, which is not a missing row.
func (s *Store) requireRow(ctx context.Context, viewID, userID int) error {
	var count int64

	err := s.conn(ctx).Model(new(model)).Where(whereRow, viewID, userID).Count(&count).Error
	if err != nil {
		return err
	}

	if count == 0 {
		return core.ErrNotFound
	}

	return nil
}

func (s *Store) conn(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx.WithContext(ctx)
	}

	return s.db.WithContext(ctx)
}
