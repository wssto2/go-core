package database

import (
	"gorm.io/gorm"
)

// Active filters records where active = 1 (or true).
func Active(db *gorm.DB) *gorm.DB {
	return db.Where("active = ?", 1)
}

// Inactive filters records where active = 0 (or false).
func Inactive(db *gorm.DB) *gorm.DB {
	return db.Where("active = ?", 0)
}

// NotDeleted filters records where del = 0 (custom soft delete).
func NotDeleted(db *gorm.DB) *gorm.DB {
	return db.Where("del = ?", 0)
}

// Ordered orders records by 'rbr' ascending.
func Ordered(db *gorm.DB) *gorm.DB {
	return db.Order("rbr asc")
}
