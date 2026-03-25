package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// GetNextOrderNumber returns the next available order_number for the given table,
// optionally scoped to a parent record. The read is wrapped in a serialisable
// SELECT … FOR UPDATE to prevent duplicate numbers under concurrent inserts.
//
// The caller must call this within an open transaction — the lock is only held
// for the duration of that transaction. Passing a non-transactional *gorm.DB
// is safe but provides no isolation guarantee.
func GetNextOrderNumber[T any](db *gorm.DB, parentColumn string, parentID int) (int, error) {
	var maxOrderNumber int
	var model T

	query := db.Model(&model).
		Select("COALESCE(MAX(order_number), 0)").
		Set("gorm:query_option", "FOR UPDATE") // row-level lock

	if parentColumn != "" && parentID > 0 {
		query = query.Where(fmt.Sprintf("`%s` = ?", parentColumn), parentID)
	}

	if err := query.Scan(&maxOrderNumber).Error; err != nil {
		return 0, err
	}

	return maxOrderNumber + 1, nil
}

// GetNextDocumentNumberAndYear returns the next document number for the current
// calendar year. The read is wrapped in SELECT … FOR UPDATE to prevent two
// concurrent requests from receiving the same number.
//
// Call this inside a transaction so the lock is held until the INSERT completes.
func GetNextDocumentNumberAndYear[T any](db *gorm.DB, numberColumn string, yearColumn string) (documentNumber int, year int, err error) {
	var model T
	year = time.Now().Year()

	err = db.Model(&model).
		Select(fmt.Sprintf("COALESCE(MAX(`%s`), 0)", numberColumn)).
		Where(fmt.Sprintf("%s = ?", EscapeColumn(yearColumn)), year).
		Set("gorm:query_option", "FOR UPDATE").
		Scan(&documentNumber).Error

	if err != nil {
		return 0, 0, err
	}

	return documentNumber + 1, year, nil
}

// ApplyFullTextSearch adds WHERE clauses for a "search all columns" feature.
// It splits the search term by spaces and checks if ANY column matches ANY part.
// Note: This uses LIKE %...% which is slow for large datasets.
func ApplyFullTextSearch(query *gorm.DB, columns []string, searchTerm string) *gorm.DB {
	if searchTerm == "" || len(columns) == 0 {
		return query
	}

	parts := strings.Fields(strings.TrimSpace(searchTerm))

	for _, part := range parts {
		if part == "" {
			continue
		}
		part = EscapeLike(part)

		var likeConditions []string
		var values []any

		for _, column := range columns {
			// Backtick-quote the column name — never interpolate user-controlled strings
			likeConditions = append(likeConditions, fmt.Sprintf("`%s` LIKE ?", column))
			values = append(values, "%"+part+"%")
		}

		query = query.Where("("+strings.Join(likeConditions, " OR ")+")", values...)
	}

	return query
}

func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

func EscapeColumn(s string) string {
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}
