package database

import (
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// MySQL's error numbers, the same on MariaDB.
const (
	mysqlErrDuplicateEntry = 1062 // ER_DUP_ENTRY
	mysqlErrLockDeadlock   = 1213 // ER_LOCK_DEADLOCK
)

// IsDuplicateKey reports a write refused by a unique key: MySQL and MariaDB by error number, SQLite and gorm's
// translated error (gorm.ErrDuplicatedKey, when TranslateError is on) by what they say.
func IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	if mysqlErr, ok := errors.AsType[*mysql.MySQLError](err); ok {
		return mysqlErr.Number == mysqlErrDuplicateEntry
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "unique constraint failed") || strings.Contains(msg, "duplicate entry") ||
		strings.Contains(msg, "duplicate key value")
}

// IsDeadlock reports a statement whose transaction InnoDB rolled back as the victim of a deadlock. Nothing of the
// transaction was written, so running it again from the start is safe.
func IsDeadlock(err error) bool {
	mysqlErr, ok := errors.AsType[*mysql.MySQLError](err)

	return ok && mysqlErr.Number == mysqlErrLockDeadlock
}

// IsDuplicateKeyNamed reports a write refused by one particular unique key, for a table with more than one. MySQL
// names the key at the end of the message ("Duplicate entry '7' for key 'orders.uq_number'", MariaDB without the
// table), so key is matched anywhere in it: pass the index name.
func IsDuplicateKeyNamed(err error, key string) bool {
	mysqlErr, ok := errors.AsType[*mysql.MySQLError](err)

	return ok && mysqlErr.Number == mysqlErrDuplicateEntry && strings.Contains(mysqlErr.Message, key)
}

// CreateUnlessDuplicate inserts value and reports whether it did: a row a unique key refuses is not an error but
// "not created". It is a plain INSERT, deliberately not GORM's OnConflict{DoNothing}: on MySQL that is
// INSERT … ON DUPLICATE KEY UPDATE pk = pk, and under CLIENT_FOUND_ROWS (which the go-core DSN sets) a duplicate
// reports one affected row, exactly as an insert does, so its count cannot tell the two apart. Outside a
// transaction only: on PostgreSQL a refused insert would abort the transaction it ran in.
func CreateUnlessDuplicate(db *gorm.DB, value any) (bool, error) {
	err := db.Create(value).Error
	if err == nil {
		return true, nil
	}

	if IsDuplicateKey(err) {
		return false, nil
	}

	return false, err
}
