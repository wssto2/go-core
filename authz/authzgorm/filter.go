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

// Columns maps an access set onto one table.
//
// Column names must be hardcoded identifiers (optionally table-qualified), never
// user input. Attribute expressions are trusted SQL written in code, for example
// a CASE over a joined column; only the values compared against them are bound
// as arguments.
type Columns struct {
	// Levels maps a hierarchy level to the column holding that level's ID, for
	// example {"dealer": "dealer_id", "location": "location_id"}. A clause is
	// constrained by every level of its chain that the table has a column for,
	// so a location binding also pins the dealer (the tenant wall). A table with
	// none of a clause's levels cannot be narrowed to that clause, and the clause
	// matches nothing.
	Levels map[string]string
	// Owner is the column holding the owning user's ID (Own qualifier). NULL and
	// zero both mean unowned. Without it, Own clauses match nothing.
	Owner string
	// OwnerLocation is the column holding the record's location ID (OwnLocation
	// qualifier). Without it, OwnLocation clauses match nothing.
	OwnerLocation string
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
	for level, col := range c.Levels {
		if !identifier.MatchString(col) {
			return bad("level "+level, col)
		}
	}
	if c.Owner != "" && !identifier.MatchString(c.Owner) {
		return bad("owner", c.Owner)
	}
	if c.OwnerLocation != "" && !identifier.MatchString(c.OwnerLocation) {
		return bad("owner location", c.OwnerLocation)
	}
	for key, expr := range c.Attrs {
		if strings.TrimSpace(expr) == "" {
			return fmt.Errorf("authzgorm: attribute %q has an empty expression", key)
		}
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
			if col := cols.Levels[s.Level]; col != "" && s.ID != 0 {
				conds = append(conds, quote(col)+" = ?")
				args = append(args, s.ID)
				pinned = true
			}
		}
		if !pinned {
			return "", nil, false
		}
	}

	switch c.Qualifier {
	case authz.QualifierAll:
	case authz.QualifierOwnLocation:
		if cols.OwnerLocation == "" || access.Principal.Location == 0 {
			return "", nil, false
		}
		conds = append(conds, quote(cols.OwnerLocation)+" = ?")
		args = append(args, access.Principal.Location)
	case authz.QualifierOwn:
		if cols.Owner == "" || access.Principal.Kind != authz.KindUser {
			return "", nil, false
		}
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
