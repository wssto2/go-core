package gormstore

import "time"

// The tables are roles, role_permissions and role_bindings. Predefined
// (code-defined) roles need no rows: a binding refers to them by role_key.

type roleModel struct {
	ID          int    `gorm:"primaryKey"`
	Name        string `gorm:"size:100;not null"`
	Description string `gorm:"size:255;not null;default:''"`
	// Attrs is the role's attribute constraints as JSON (an object of string arrays).
	Attrs     string `gorm:"type:text"`
	CreatedBy int    `gorm:"not null;default:0"`
	UpdatedBy int    `gorm:"not null;default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (roleModel) TableName() string { return "roles" }

type rolePermissionModel struct {
	RoleID     int    `gorm:"primaryKey;autoIncrement:false"`
	Permission string `gorm:"primaryKey;size:100"`
	Qualifier  string `gorm:"size:16;not null"`
}

func (rolePermissionModel) TableName() string { return "role_permissions" }

// roleBindingModel stores a binding. A custom role is referenced by role_id and
// a predefined one by role_key; the unused one holds 0 or ” (not NULL) so the
// unique index also guards against duplicates.
type roleBindingModel struct {
	ID          int       `gorm:"primaryKey"`
	SubjectKind string    `gorm:"size:16;not null;uniqueIndex:uq_role_bindings,priority:1;index:idx_role_bindings_subject,priority:1"`
	SubjectID   int       `gorm:"not null;uniqueIndex:uq_role_bindings,priority:2;index:idx_role_bindings_subject,priority:2"`
	RoleID      int       `gorm:"not null;default:0;uniqueIndex:uq_role_bindings,priority:3;index:idx_role_bindings_role;check:chk_role_bindings_role,(role_id > 0 AND role_key = '') OR (role_id = 0 AND role_key <> '')"`
	RoleKey     string    `gorm:"size:64;not null;default:'';uniqueIndex:uq_role_bindings,priority:4"`
	ScopeLevel  string    `gorm:"size:32;not null;uniqueIndex:uq_role_bindings,priority:5"`
	ScopeID     int       `gorm:"not null;default:0;uniqueIndex:uq_role_bindings,priority:6"`
	CreatedBy   int       `gorm:"not null;default:0"`
	CreatedAt   time.Time `gorm:"not null"`
}

func (roleBindingModel) TableName() string { return "role_bindings" }
