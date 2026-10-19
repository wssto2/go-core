package database

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

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
			likeConditions = append(likeConditions, fmt.Sprintf("%s LIKE ?", QuoteColumn(column)))
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

// EscapeBackticks escapes backticks in a column name for safe use in SQL identifiers.
func EscapeBackticks(s string) string {
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}

// QuoteColumn returns the column name wrapped in backticks, escaping any backticks inside.
func QuoteColumn(s string) string {
	escaped := EscapeBackticks(s)
	return "`" + escaped + "`"
}
