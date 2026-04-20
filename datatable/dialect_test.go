package datatable

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuoteIdent_MySQL(t *testing.T) {
	assert.Equal(t, "`name`", quoteIdent("mysql", "name"))
	assert.Equal(t, "`na``me`", quoteIdent("mysql", "na`me")) // escape existing backticks
}

func TestQuoteIdent_SQLite(t *testing.T) {
	assert.Equal(t, "`id`", quoteIdent("sqlite", "id"))
}

func TestQuoteIdent_Postgres(t *testing.T) {
	assert.Equal(t, `"name"`, quoteIdent("postgres", "name"))
	assert.Equal(t, `"na""me"`, quoteIdent("postgres", `na"me`)) // escape existing double quotes
}

func TestLikeOp_MySQL(t *testing.T) {
	assert.Equal(t, "LIKE", likeOp("mysql"))
}

func TestLikeOp_SQLite(t *testing.T) {
	assert.Equal(t, "LIKE", likeOp("sqlite"))
}

func TestLikeOp_Postgres(t *testing.T) {
	assert.Equal(t, "ILIKE", likeOp("postgres"))
}

func TestConcatExpr_MySQL(t *testing.T) {
	expr := concatExpr("mysql", "t", []string{"first_name", "last_name"})
	assert.Equal(t, "CONCAT(COALESCE(t.`first_name`, ''), ' ', COALESCE(t.`last_name`, ''))", expr)
}

func TestConcatExpr_SQLite(t *testing.T) {
	expr := concatExpr("sqlite", "t", []string{"first_name", "last_name"})
	assert.Equal(t, "COALESCE(t.`first_name`, '') || ' ' || COALESCE(t.`last_name`, '')", expr)
}

func TestConcatExpr_Postgres(t *testing.T) {
	expr := concatExpr("postgres", "t", []string{"first_name", "last_name"})
	assert.Equal(t, `COALESCE(t."first_name", '') || ' ' || COALESCE(t."last_name", '')`, expr)
}

func TestConcatExpr_ThreeColumns_MySQL(t *testing.T) {
	expr := concatExpr("mysql", "users", []string{"a", "b", "c"})
	assert.Equal(t, "CONCAT(COALESCE(users.`a`, ''), ' ', COALESCE(users.`b`, ''), ' ', COALESCE(users.`c`, ''))", expr)
}

func TestConcatWSExpr_MySQL(t *testing.T) {
	expr := concatWSExpr("mysql", "/", "t", []string{"year", "no"})
	assert.Equal(t, "CONCAT_WS('/', COALESCE(t.`year`, ''), COALESCE(t.`no`, ''))", expr)
}

func TestConcatWSExpr_SQLite(t *testing.T) {
	expr := concatWSExpr("sqlite", "/", "t", []string{"year", "no"})
	assert.Equal(t, "COALESCE(t.`year`, '') || '/' || COALESCE(t.`no`, '')", expr)
}

func TestConcatWSExpr_Postgres(t *testing.T) {
	expr := concatWSExpr("postgres", "/", "t", []string{"year", "no"})
	assert.Equal(t, `COALESCE(t."year", '') || '/' || COALESCE(t."no", '')`, expr)
}

func TestConcatWSExpr_EscapesSingleQuoteInSep_MySQL(t *testing.T) {
	expr := concatWSExpr("mysql", "'", "t", []string{"a"})
	assert.Contains(t, expr, "''") // separator ' should be escaped to ''
}

func TestConcatWSExpr_EscapesSingleQuoteInSep_SQLite(t *testing.T) {
	expr := concatWSExpr("sqlite", "'", "t", []string{"a", "b"})
	assert.Contains(t, expr, "''")
}
