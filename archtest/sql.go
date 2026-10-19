package archtest

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

const gormClauseImport = "gorm.io/gorm/clause"

// SQL switches on the SQLPortability rule.
type SQL struct {
	// Dirs are the directories, relative to the root, whose production Go
	// files are scanned. Empty leaves the rule off.
	Dirs []string
}

// SQLRule is one construct that MariaDB 10.3 rejects, or that the house rules
// forbid so a query cannot depend on it.
type SQLRule struct {
	Pattern *regexp.Regexp
	What    string
}

// SQLRules is the table both SQLPortability and dbtest.Portability hold SQL to.
// Every market runs MariaDB 10.3.39, which rejects these MySQL 8 / newer
// MariaDB constructs with Error 1064, while a local MySQL accepts them, so
// nothing else would catch one. Rewrite the query instead: a single-table
// SELECT ... FOR UPDATE by primary key replaces SKIP LOCKED and FOR UPDATE OF;
// FOR UPDATE (or LOCK IN SHARE MODE) replaces FOR SHARE. Window functions are
// fine: MariaDB has them since 10.2.
func SQLRules() []SQLRule {
	return []SQLRule{
		{regexp.MustCompile(`(?i)\bSKIP\s+LOCKED\b`), "SKIP LOCKED (MariaDB 10.6+)"},
		{regexp.MustCompile(`(?i)\bFOR\s+UPDATE\s+OF\b`), "FOR UPDATE OF <table> (not in MariaDB)"},
		{regexp.MustCompile(`(?i)\bFOR\s+SHARE\b`), "FOR SHARE (MySQL 8; use LOCK IN SHARE MODE or FOR UPDATE)"},
		{regexp.MustCompile(`(?i)\bJSON_TABLE\s*\(`), "JSON_TABLE (MariaDB 10.6+)"},
		{regexp.MustCompile(`->>`), "->> (not in MariaDB)"},
		{regexp.MustCompile(`(?i)\bRETURNING\b`), "RETURNING (MariaDB 10.5+)"},
	}
}

// sqlPortability: the string literals of the configured directories against
// SQLRules, and the gorm clause.Locking forms that render a rejected
// construct: Table renders FOR UPDATE OF, Strength "SHARE" renders FOR SHARE,
// and the constants LockingStrengthShare and LockingOptionsSkipLocked.
func (p *project) sqlPortability() []finding {
	table := goLiteralRules()

	var out []finding

	for _, src := range p.productionIn(p.cfg.SQL.Dirs...) {
		clauseAlias := aliasOf(src.File, gormClauseImport)

		report := func(node ast.Node, what string) {
			out = append(out, finding{key: src.Rel, text: src.At(node) + "  " + what})
		}

		ast.Inspect(src.File, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.BasicLit:
				if typed.Kind == token.STRING {
					literalViolations(typed, table, report)
				}
			case *ast.SelectorExpr:
				if what, bad := forbiddenClauseConst(typed, clauseAlias); bad {
					report(typed, what)
				}
			case *ast.CompositeLit:
				lockingViolations(typed, clauseAlias, report)
			}

			return true
		})
	}

	return out
}

// goLiteralRules is SQLRules for Go string literals, where RETURNING counts only
// in capitals: "returning" is common English in messages, while SQL in Go is
// written in capitals.
func goLiteralRules() []SQLRule {
	table := SQLRules()

	for i := range table {
		if strings.HasPrefix(table[i].What, "RETURNING") {
			table[i].Pattern = regexp.MustCompile(`\bRETURNING\b`)
		}
	}

	return table
}

func literalViolations(lit *ast.BasicLit, table []SQLRule, report func(ast.Node, string)) {
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		value = lit.Value
	}

	for _, rule := range table {
		if rule.Pattern.MatchString(value) {
			report(lit, rule.What)
		}
	}
}

// forbiddenClauseConst is clause.LockingStrengthShare and clause.LockingOptionsSkipLocked.
func forbiddenClauseConst(sel *ast.SelectorExpr, clauseAlias string) (string, bool) {
	if clauseAlias == "" || !isIdent(sel.X, clauseAlias) {
		return "", false
	}

	switch sel.Sel.Name {
	case "LockingOptionsSkipLocked":
		return "SKIP LOCKED (MariaDB 10.6+)", true
	case "LockingStrengthShare":
		return "FOR SHARE (MySQL 8; use LOCK IN SHARE MODE or FOR UPDATE)", true
	}

	return "", false
}

// lockingViolations reads a clause.Locking literal. Options: "SKIP LOCKED" is
// a string literal and is found as one.
func lockingViolations(lit *ast.CompositeLit, clauseAlias string, report func(ast.Node, string)) {
	sel, isSel := lit.Type.(*ast.SelectorExpr)
	if clauseAlias == "" || !isSel || sel.Sel.Name != "Locking" || !isIdent(sel.X, clauseAlias) {
		return
	}

	for _, elt := range lit.Elts {
		pair, isPair := elt.(*ast.KeyValueExpr)
		if !isPair {
			continue
		}

		value, _ := StringLit(pair.Value)

		switch {
		case isIdent(pair.Key, "Table"):
			report(pair, "clause.Locking{Table: ...} renders FOR UPDATE OF <table> (not in MariaDB)")
		case isIdent(pair.Key, "Strength") && strings.EqualFold(value, "SHARE"):
			report(pair, `clause.Locking{Strength: "SHARE"} renders FOR SHARE (MySQL 8)`)
		}
	}
}
