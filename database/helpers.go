package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/wssto2/go-core/utils"
	"gorm.io/gorm"
)

// GetNextOrderNumber calculates the next order number for a given table (T).
// It checks the maximum existing 'order_number' and increments it.
// Optional: parentColumn/parentID to scope the query (e.g. order number within a category).
func GetNextOrderNumber[T any](db *gorm.DB, parentColumn string, parentID int) (int, error) {
	var maxOrderNumber int
	var model T

	query := db.Model(&model).Select("COALESCE(MAX(order_number), 0)")

	if parentColumn != "" && parentID > 0 {
		query = query.Where(fmt.Sprintf("%s = ?", parentColumn), parentID)
	}

	err := query.Scan(&maxOrderNumber).Error
	if err != nil {
		return 0, err
	}
	return maxOrderNumber + 1, nil
}

// GetFormOrderNumberAndYear calculates the next form number for the current year.
// Assumes columns: 'br_obrasca' (int) and 'godina_obrasca' (int).
func GetFormOrderNumberAndYear[T any](db *gorm.DB) (orderNumber int, year int, err error) {
	var model T
	year = time.Now().Year()

	err = db.Model(&model).
		Select("COALESCE(MAX(br_obrasca), 0)").
		Where("godina_obrasca = ?", year).
		Scan(&orderNumber).Error
	
	if err != nil {
		return 0, 0, err
	}

	return orderNumber + 1, year, nil
}

// ApplyFullTextSearch adds WHERE clauses for a "search all columns" feature.
// It splits the search term by spaces and checks if ANY column matches ANY part.
// Note: This uses LIKE %...% which is slow for large datasets.
func ApplyFullTextSearch(query *gorm.DB, columns []string, searchTerm string) *gorm.DB {
	if searchTerm == "" || len(columns) == 0 {
		return query
	}

	safeSearchTerm := utils.EscapeLike(searchTerm)
	parts := strings.Fields(safeSearchTerm)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		var likeConditions []string
		for _, column := range columns {
			likeConditions = append(likeConditions, fmt.Sprintf("%s LIKE '%%%s%%'", column, part))
		}
		
		// (col1 LIKE %part% OR col2 LIKE %part%)
		query = query.Where(strings.Join(likeConditions, " OR "))
	}
	
	return query
}
