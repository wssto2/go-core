package auth

import (
	"database/sql/driver"
	"errors"
	"time"

	"github.com/goccy/go-json"
	"github.com/wssto2/go-core/database/types"
)

type AccountType struct {
	ID            int                `json:"id" gorm:"primarykey"`
	Name          string             `json:"name" gorm:"size:50;not null"`
	NameI18n      types.I18n         `json:"name_i18n" gorm:"serializer:json"`
	Description   types.NullString   `json:"description" gorm:"size:255"`
	Policies      []string           `json:"policies" gorm:"type:json;serializer:json"`
	Active        bool               `json:"active" gorm:"not null;default:false;index"`
	CreatedBy     int                `json:"created_by" gorm:"type:mediumint unsigned;not null;"`
	UpdatedBy     types.NullInt      `json:"updated_by" gorm:"type:mediumint unsigned;"`
	ActivatedBy   types.NullInt      `json:"activated_by" gorm:"type:mediumint unsigned;"`
	DeactivatedBy types.NullInt      `json:"deactivated_by" gorm:"type:mediumint unsigned;"`
	DeletedBy     types.NullInt      `json:"deleted_by" gorm:"type:mediumint unsigned;"`
	CreatedAt     time.Time          `json:"created_at" gorm:"null"`
	UpdatedAt     types.NullDateTime `json:"updated_at" gorm:"null"`
	ActivatedAt   types.NullDateTime `json:"activated_at" gorm:"null"`
	DeactivatedAt types.NullDateTime `json:"deactivated_at" gorm:"null"`
	DeletedAt     types.NullDateTime `json:"deleted_at" gorm:"null"`
}

type AccountRole struct {
	ID            int                `json:"id" gorm:"primarykey"`
	Name          string             `json:"name" gorm:"size:50;not null"`
	NameI18n      types.I18n         `json:"name_i18n" gorm:"serializer:json"`
	Description   types.NullString   `json:"description" gorm:"size:255"`
	Policies      []string           `json:"policies" gorm:"type:json;serializer:json"`
	Active        bool               `json:"active" gorm:"not null;default:false;index"`
	CreatedBy     int                `json:"created_by" gorm:"type:mediumint unsigned;not null;"`
	UpdatedBy     types.NullInt      `json:"updated_by" gorm:"type:mediumint unsigned;"`
	ActivatedBy   types.NullInt      `json:"activated_by" gorm:"type:mediumint unsigned;"`
	DeactivatedBy types.NullInt      `json:"deactivated_by" gorm:"type:mediumint unsigned;"`
	DeletedBy     types.NullInt      `json:"deleted_by" gorm:"type:mediumint unsigned;"`
	CreatedAt     time.Time          `json:"created_at" gorm:"null"`
	UpdatedAt     types.NullDateTime `json:"updated_at" gorm:"null"`
	ActivatedAt   types.NullDateTime `json:"activated_at" gorm:"null"`
	DeactivatedAt types.NullDateTime `json:"deactivated_at" gorm:"null"`
	DeletedAt     types.NullDateTime `json:"deleted_at" gorm:"null"`
}

type UserPreferences struct {
	DarkMode bool `json:"dark_mode"`
}

func (p UserPreferences) Value() (driver.Value, error) {
	return json.Marshal(p)
}

func (p *UserPreferences) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(bytes, p)
}
