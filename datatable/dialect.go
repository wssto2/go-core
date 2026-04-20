package datatable

import (
	"fmt"
	"strings"
)

// quoteIdent quotes a SQL identifier for the active dialect.
//
//   MySQL / SQLite: `ident`
//   PostgreSQL:     "ident"
func quoteIdent(dialect, s string) string {
	switch dialect {
	case "postgres":
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	default: // mysql, sqlite — both accept backtick quoting
		return "`" + strings.ReplaceAll(s, "`", "``") + "`"
	}
}

// likeOp returns the case-insensitive LIKE operator for the active dialect.
// PostgreSQL uses ILIKE; MySQL/SQLite default to case-insensitive LIKE.
func likeOp(dialect string) string {
	if dialect == "postgres" {
		return "ILIKE"
	}
	return "LIKE"
}

// concatExpr builds a space-joined multi-column string expression for search.
// Uses COALESCE (SQL standard) so that NULL columns contribute an empty string.
//
//	MySQL:      CONCAT(COALESCE(t.a, ''), ' ', COALESCE(t.b, ''))
//	SQLite/PG:  COALESCE(t.a, '') || ' ' || COALESCE(t.b, '')
func concatExpr(dialect, tableName string, cols []string) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = fmt.Sprintf("COALESCE(%s.%s, '')", tableName, quoteIdent(dialect, col))
	}
	if dialect == "mysql" {
		// Interleave a literal space between each column.
		all := make([]string, 0, len(parts)*2-1)
		for i, p := range parts {
			all = append(all, p)
			if i < len(parts)-1 {
				all = append(all, "' '")
			}
		}
		return "CONCAT(" + strings.Join(all, ", ") + ")"
	}
	// SQLite and PostgreSQL: use || operator.
	return strings.Join(parts, " || ' ' || ")
}

// concatWSExpr builds a separator-joined multi-column string expression.
//
//	MySQL:      CONCAT_WS('sep', COALESCE(t.a, ''), COALESCE(t.b, ''))
//	SQLite/PG:  COALESCE(t.a, '') || 'sep' || COALESCE(t.b, '')
func concatWSExpr(dialect, sep, tableName string, cols []string) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = fmt.Sprintf("COALESCE(%s.%s, '')", tableName, quoteIdent(dialect, col))
	}
	if dialect == "mysql" {
		escapedSep := strings.ReplaceAll(sep, "'", "''")
		return fmt.Sprintf("CONCAT_WS('%s', %s)", escapedSep, strings.Join(parts, ", "))
	}
	// SQLite and PostgreSQL: use || operator with a literal separator.
	escapedSep := strings.ReplaceAll(sep, "'", "''")
	sepLiteral := fmt.Sprintf("'%s'", escapedSep)
	return strings.Join(parts, " || "+sepLiteral+" || ")
}
