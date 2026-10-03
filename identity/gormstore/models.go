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
	Email        *string   `gorm:"size:255;uniqueIndex:uq_accounts_email"`
	Phone        string    `gorm:"size:30;not null;default:''"`
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

// codeModel is user_verification_codes as arv-next created it: unsigned
// integers, DATETIME without fractions, utf8mb3.
type codeModel struct {
	ID            uint64     `gorm:"primaryKey;autoIncrement"`
	UserID        uint32     `gorm:"not null;index:user_verification_codes_user_purpose_created,priority:1"`
	Purpose       string     `gorm:"size:32;not null;index:user_verification_codes_user_purpose_created,priority:2"`
	Target        *string    `gorm:"size:255"`
	CodeHash      string     `gorm:"type:char(64);not null"`
	Attempts      uint8      `gorm:"not null;default:0"`
	ExpiresAt     time.Time  `gorm:"type:datetime;not null"`
	ConsumedAt    *time.Time `gorm:"type:datetime"`
	InvalidatedAt *time.Time `gorm:"type:datetime"`
	CreatedIP     *string    `gorm:"size:45"`
	CreatedAt     time.Time  `gorm:"type:datetime;not null;index:user_verification_codes_user_purpose_created,priority:3"`
	UpdatedAt     time.Time  `gorm:"type:datetime;not null"`
}

func (codeModel) TableName() string { return "user_verification_codes" }

// reauthModel is user_reauth_attempts.
type reauthModel struct {
	UserID      uint32     `gorm:"primaryKey;autoIncrement:false"`
	Failures    uint16     `gorm:"not null;default:0"`
	LockedUntil *time.Time `gorm:"type:datetime"`
	UpdatedAt   time.Time  `gorm:"type:datetime;not null"`
}

func (reauthModel) TableName() string { return "user_reauth_attempts" }

// Migrate creates the identity tables from the GORM models. It is for tests,
// where it is quicker than the SQL files and runs on SQLite; production runs
// the files in identity/migrations, which schema_test.go holds equal to these
// models (on MySQL and MariaDB).
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&accountModel{}, &signInModel{}, &auth.Token{}, &codeModel{}, &reauthModel{})
}
