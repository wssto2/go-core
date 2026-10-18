package repository

import "gorm.io/gorm/clause"

var (
	a  = clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}
	b  = clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "o"}}
	c  = clause.Locking{Strength: "SHARE"}
	d  = clause.Locking{Strength: clause.LockingStrengthShare}
	e  = clause.Locking{Strength: "UPDATE", Options: clause.LockingOptionsSkipLocked}
	f  = "SELECT id FROM t WHERE x = 1 FOR UPDATE OF t"
	g  = "SELECT j.id FROM JSON_TABLE(doc, '$[0]' COLUMNS (id INT PATH '$')) j"
	h  = "SELECT doc->>'$.id' FROM t"
	i  = "SELECT id FROM t FOR SHARE"
	j  = "INSERT INTO t (a) VALUES (1) RETURNING id"
	ok = clause.Locking{Strength: "UPDATE"}
	k  = "SELECT id FROM t WHERE id = ? FOR UPDATE" // FOR SHARE in a comment is not SQL
	l  = "SELECT id FROM t LOCK IN SHARE MODE"
	m  = "SELECT id FROM t FOR UPDATE NOWAIT"
	n  = "returning nil: the order has no lines" // prose, not SQL
)
