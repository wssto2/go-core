// Package authzgorm turns an authz.AccessSet into a WHERE clause, so a list
// query returns exactly the rows the principal may see.
//
// The SQL is plain: equality, IN, IS NULL, AND and OR. It has no JSON functions,
// CTEs, or locking clauses, so it runs unchanged on MariaDB 10.3, MySQL and SQLite.
package authzgorm

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/wssto2/go-core/authz"
	"gorm.io/gorm"
)

// Expr is trusted SQL written in code, with ? placeholders and their arguments.
// It is never built from user input; only Args are bound values.
type Expr struct {
	SQL  string
	Args []any
}

// OwnerRequest is what OwnerMatch is asked to express.
type OwnerRequest struct {
	// UserID is the principal's ID.
	UserID int
	// UnownedIsOwn is the permission's flag: records with no owner also match.
	UnownedIsOwn bool
}

// Columns maps an access set onto one table.
//
// Plain columns (Levels, Owner, OwnerLocation) are hardcoded identifiers,
// optionally table-qualified, validated and quoted. When a table cannot say it
// with one column (the owner lives in another table, the location is the
// creator's), use the trusted-SQL counterpart instead: LevelExprs,
// OwnerLocationExpr, OwnerMatch, Attrs. Never build any of these from user
// input; only the compared values are bound as arguments.
type Columns struct {
	// Levels maps a hierarchy level to the column holding that level's ID, for
	// example {"dealer": "dealer_id", "location": "location_id"}. A clause is
	// constrained by every level of its chain the table can express, so a
	// location binding also pins the dealer (the tenant wall). A table with none
	// of a clause's levels cannot be narrowed to that clause, and the clause
	// matches nothing.
	Levels map[string]string
	// LevelExprs maps a level to a SQL expression yielding that level's ID, for
	// levels a table reaches through another table, for example
	// "(SELECT location_id FROM users WHERE users.id = offers.created_by)". It is
	// compared as (expr) = ?. A level must not be in both Levels and LevelExprs.
	LevelExprs map[string]string
	// Owner is the column holding the owning user's ID (Own qualifier). NULL and
	// zero both mean unowned.
	Owner string
	// OwnerMatch expresses the Own qualifier as an arbitrary predicate, for an
	// owner that is not a column of this table. It returns the predicate and its
	// arguments, or false when it cannot be expressed (the clause then matches
	// nothing). Set at most one of Owner and OwnerMatch. Without either, Own
	// clauses match nothing.
	OwnerMatch func(OwnerRequest) (Expr, bool)
	// OwnerLocation is the column holding the record's location ID (OwnLocation
	// qualifier).
	OwnerLocation string
	// OwnerLocationExpr is a SQL expression yielding the record's location ID,
	// compared as (expr) = ?. Set at most one of OwnerLocation and
	// OwnerLocationExpr. Without either, OwnLocation clauses match nothing.
	OwnerLocationExpr string
	// Attrs maps an attribute key to the SQL expression yielding its value on
	// this table. A role constraint on an attribute the table cannot express
	// makes that clause match nothing.
	Attrs map[string]string
}

// Condition is the result of Build.
type Condition struct {
	// SQL is a parenthesized boolean expression with ? placeholders; empty when
	// All or None.
	SQL  string
	Args []any
	// All means no restriction: add no WHERE. None means nothing matches.
	All, None bool
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// Filter returns a GORM scope restricting a query to the access set:
//
//	access, _ := engine.Access(ctx, "crm.lead:view")
//	db.Scopes(authzgorm.Filter(access, leadColumns)).Find(&leads)
//
// An empty access set, or one that cannot be expressed on the table, yields a
// query that matches nothing. A misconfigured Columns adds an error to the
// query and also matches nothing.
func Filter(access authz.AccessSet, cols Columns) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		cond, err := Build(access, cols, func(name string) string { return db.Statement.Quote(name) })
		if err != nil {
			_ = db.AddError(err)
			return db.Where("1 = 0")
		}
		switch {
		case cond.All:
			return db
		case cond.None:
			return db.Where("1 = 0")
		}
		return db.Where(cond.SQL, cond.Args...)
	}
}

// Build computes the condition without a database. quote quotes an identifier;
// nil leaves identifiers as they are.
func Build(access authz.AccessSet, cols Columns, quote func(string) string) (Condition, error) {
	if quote == nil {
		quote = func(s string) string { return s }
	}
	if err := cols.check(); err != nil {
		return Condition{}, err
	}
	var parts []string
	var args []any
	for _, c := range access.Clauses {
		sql, a, ok := clauseSQL(access, c, cols, quote)
		if !ok {
			continue // cannot match anything on this table
		}
		if sql == "" {
			return Condition{All: true}, nil
		}
		parts = append(parts, "("+sql+")")
		args = append(args, a...)
	}
	if len(parts) == 0 {
		return Condition{None: true}, nil
	}
	return Condition{SQL: "(" + strings.Join(parts, " OR ") + ")", Args: args}, nil
}

func (c Columns) check() error {
	bad := func(kind, name string) error {
		return fmt.Errorf("authzgorm: %s column %q is not a plain identifier", kind, name)
	}
	both := func(what string) error {
		return fmt.Errorf("authzgorm: %s is set twice (plain column and expression)", what)
	}
	for level, col := range c.Levels {
		if !identifier.MatchString(col) {
			return bad("level "+level, col)
		}
		if _, dup := c.LevelExprs[level]; dup {
			return both("level " + level)
		}
	}
	for level, expr := range c.LevelExprs {
		if err := checkValueExpr("level "+level, expr); err != nil {
			return err
		}
	}
	if c.Owner != "" && !identifier.MatchString(c.Owner) {
		return bad("owner", c.Owner)
	}
	if c.Owner != "" && c.OwnerMatch != nil {
		return both("owner")
	}
	if c.OwnerLocation != "" && !identifier.MatchString(c.OwnerLocation) {
		return bad("owner location", c.OwnerLocation)
	}
	if c.OwnerLocation != "" && c.OwnerLocationExpr != "" {
		return both("owner location")
	}
	if c.OwnerLocationExpr != "" {
		if err := checkValueExpr("owner location", c.OwnerLocationExpr); err != nil {
			return err
		}
	}
	for key, expr := range c.Attrs {
		if err := checkValueExpr("attribute "+key, expr); err != nil {
			return err
		}
	}
	return nil
}

// checkValueExpr validates an expression that yields a value: non-empty, and no
// placeholders (its compared value is bound by the filter, not by the caller).
func checkValueExpr(what, expr string) error {
	if strings.TrimSpace(expr) == "" {
		return fmt.Errorf("authzgorm: %s has an empty expression", what)
	}
	if strings.Contains(expr, "?") {
		return fmt.Errorf("authzgorm: %s expression must not contain placeholders", what)
	}
	return nil
}

// clauseSQL builds one clause: AND of scope, qualifier and attribute
// conditions. ok is false when the clause cannot match anything on the table;
// an empty sql with ok true means the clause is unrestricted.
func clauseSQL(access authz.AccessSet, c authz.Clause, cols Columns, quote func(string) string) (sql string, args []any, ok bool) {
	var conds []string

	if c.Scope.ID != 0 { // below the root: pin every level the table has a column for
		chain := c.Chain
		if len(chain) == 0 {
			chain = authz.Chain{c.Scope}
		}
		pinned := false
		for _, s := range chain {
			if s.ID == 0 {
				continue
			}
			if col := cols.Levels[s.Level]; col != "" {
				conds = append(conds, quote(col)+" = ?")
			} else if expr := cols.LevelExprs[s.Level]; expr != "" {
				conds = append(conds, "("+expr+") = ?")
			} else {
				continue
			}
			args = append(args, s.ID)
			pinned = true
		}
		if !pinned {
			return "", nil, false
		}
	}

	switch c.Qualifier {
	case authz.QualifierAll:
	case authz.QualifierOwnLocation:
		if access.Principal.Location == 0 {
			return "", nil, false
		}
		switch {
		case cols.OwnerLocation != "":
			conds = append(conds, quote(cols.OwnerLocation)+" = ?")
		case cols.OwnerLocationExpr != "":
			conds = append(conds, "("+cols.OwnerLocationExpr+") = ?")
		default:
			return "", nil, false
		}
		args = append(args, access.Principal.Location)
	case authz.QualifierOwn:
		if access.Principal.Kind != authz.KindUser {
			return "", nil, false
		}
		switch {
		case cols.OwnerMatch != nil:
			e, ok := cols.OwnerMatch(OwnerRequest{UserID: access.Principal.ID, UnownedIsOwn: access.UnownedIsOwn})
			if !ok || strings.TrimSpace(e.SQL) == "" || strings.Count(e.SQL, "?") != len(e.Args) {
				return "", nil, false // inexpressible or malformed: match nothing
			}
			conds = append(conds, "("+e.SQL+")")
			args = append(args, e.Args...)
		case cols.Owner != "":
			col := quote(cols.Owner)
			if access.UnownedIsOwn {
				conds = append(conds, "("+col+" = ? OR "+col+" IS NULL OR "+col+" = 0)")
			} else {
				conds = append(conds, col+" = ?")
			}
			args = append(args, access.Principal.ID)
		default:
			return "", nil, false
		}
	default:
		return "", nil, false
	}

	keys := make([]string, 0, len(c.Attrs))
	for k := range c.Attrs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		expr := cols.Attrs[key]
		if expr == "" {
			return "", nil, false
		}
		values := c.Attrs[key]
		if len(values) == 0 {
			return "", nil, false // "any of nothing"
		}
		conds = append(conds, "("+expr+") IN ("+strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")+")")
		for _, v := range values {
			args = append(args, v)
		}
	}
	return strings.Join(conds, " AND "), args, true
}
