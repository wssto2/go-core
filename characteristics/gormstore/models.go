package gormstore

import (
	"time"

	"gorm.io/gorm"
)

// The models mirror the migration files column for column (schema_test.go holds them equal). Their types are the least
// that lets GORM create the tables on SQLite for tests, which cannot run the MySQL DDL.

type level1 struct {
	ID            uint       `gorm:"primaryKey;autoIncrement"`
	Name          string     `gorm:"size:255;not null"`
	NameI18n      *string    `gorm:"column:name_i18n;type:longtext"`
	Meta          *string    `gorm:"type:longtext"`
	Order         uint       `gorm:"column:order;not null;default:1;index:characteristics_level1_order_index"`
	Active        bool       `gorm:"not null;default:false;index:characteristics_level1_active_index"`
	CreatedBy     *uint      `gorm:"column:created_by"`
	UpdatedBy     *uint      `gorm:"column:updated_by"`
	ActivatedBy   *uint      `gorm:"column:activated_by"`
	DeactivatedBy *uint      `gorm:"column:deactivated_by"`
	CreatedAt     *time.Time `gorm:"column:created_at;type:datetime;autoCreateTime:false"`
	UpdatedAt     *time.Time `gorm:"column:updated_at;type:datetime;autoUpdateTime:false"`
	ActivatedAt   *time.Time `gorm:"column:activated_at;type:datetime"`
	DeactivatedAt *time.Time `gorm:"column:deactivated_at;type:datetime"`
}

func (level1) TableName() string { return "characteristics_level1" }

type level2 struct {
	ID            uint       `gorm:"primaryKey;autoIncrement"`
	Level1        uint       `gorm:"column:level1_id;not null;index:characteristics_level2_level1_id_index"`
	Name          string     `gorm:"size:255;not null"`
	NameI18n      *string    `gorm:"column:name_i18n;type:longtext"`
	Meta          *string    `gorm:"type:longtext"`
	Order         uint       `gorm:"column:order;not null;default:1;index:characteristics_level2_order_index"`
	Active        bool       `gorm:"not null;default:false;index:characteristics_level2_active_index"`
	CreatedBy     *uint      `gorm:"column:created_by"`
	UpdatedBy     *uint      `gorm:"column:updated_by"`
	ActivatedBy   *uint      `gorm:"column:activated_by"`
	DeactivatedBy *uint      `gorm:"column:deactivated_by"`
	CreatedAt     *time.Time `gorm:"column:created_at;type:datetime;autoCreateTime:false"`
	UpdatedAt     *time.Time `gorm:"column:updated_at;type:datetime;autoUpdateTime:false"`
	ActivatedAt   *time.Time `gorm:"column:activated_at;type:datetime"`
	DeactivatedAt *time.Time `gorm:"column:deactivated_at;type:datetime"`
}

func (level2) TableName() string { return "characteristics_level2" }

type level3 struct {
	ID            uint       `gorm:"primaryKey;autoIncrement"`
	Level1        uint       `gorm:"column:level1_id;not null;index:characteristics_level3_level1_id_index"`
	Level2        uint       `gorm:"column:level2_id;not null;index:characteristics_level3_level2_id_index"`
	Name          string     `gorm:"size:255;not null"`
	NameI18n      *string    `gorm:"column:name_i18n;type:longtext"`
	Meta          *string    `gorm:"type:longtext"`
	Order         uint       `gorm:"column:order;not null;default:1;index:characteristics_level3_order_index"`
	Active        bool       `gorm:"not null;default:false;index:characteristics_level3_active_index"`
	CreatedBy     *uint      `gorm:"column:created_by"`
	UpdatedBy     *uint      `gorm:"column:updated_by"`
	ActivatedBy   *uint      `gorm:"column:activated_by"`
	DeactivatedBy *uint      `gorm:"column:deactivated_by"`
	CreatedAt     *time.Time `gorm:"column:created_at;type:datetime;autoCreateTime:false"`
	UpdatedAt     *time.Time `gorm:"column:updated_at;type:datetime;autoUpdateTime:false"`
	ActivatedAt   *time.Time `gorm:"column:activated_at;type:datetime"`
	DeactivatedAt *time.Time `gorm:"column:deactivated_at;type:datetime"`
}

func (level3) TableName() string { return "characteristics_level3" }

// Migrate creates the first n default tables (1 to 3) from the models, for tests on SQLite; production runs the files
// in characteristics/migrations.
func Migrate(db *gorm.DB, n int) error {
	models := []any{new(level1), new(level2), new(level3)}

	return db.AutoMigrate(models[:n]...) //nolint:wrapcheck // the caller names the step
}
