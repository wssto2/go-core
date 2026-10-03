package gormstore

import (
	"time"

	"github.com/wssto2/go-core/auth"
	"gorm.io/gorm"
)

// The models mirror the migration files column for column (schema_test.go
// holds them equal). They are private: the store maps them to account types.

type accountModel struct {
	ID           int       `gorm:"primaryKey;autoIncrement"`
	Login        string    `gorm:"size:100;not null;uniqueIndex:uq_accounts_login"`
	Email        string    `gorm:"size:255;not null"`
	Name         string    `gorm:"size:150;not null"`
	PasswordHash string    `gorm:"size:255;not null"`
	Locale       string    `gorm:"size:16;not null"`
	Active       bool      `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (accountModel) TableName() string { return "accounts" }

type signInModel struct {
	ID        int       `gorm:"primaryKey;autoIncrement;index:idx_user_signins_user,priority:2"`
	UserID    int       `gorm:"not null;index:idx_user_signins_user,priority:1"`
	Event     string    `gorm:"size:32;not null;index:idx_user_signins_event_created,priority:1"`
	IP        string    `gorm:"size:45;not null;default:''"`
	UserAgent string    `gorm:"size:255;not null;default:''"`
	ActorID   *int      `gorm:""`
	CreatedAt time.Time `gorm:"not null;index:idx_user_signins_event_created,priority:2;index:idx_user_signins_created"`
}

func (signInModel) TableName() string { return "user_signins" }

// Migrate creates the identity tables from the GORM models. It is for tests,
// where it is quicker than the SQL files and runs on SQLite; production runs
// the files in identity/migrations, which schema_test.go holds equal to these
// models (on MySQL and MariaDB).
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&accountModel{}, &signInModel{}, &auth.Token{})
}
