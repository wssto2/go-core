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

	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == mysqlErrDuplicateEntry
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "unique constraint failed") || strings.Contains(msg, "duplicate entry") ||
		strings.Contains(msg, "duplicate key value")
}

// IsDeadlock reports a statement whose transaction InnoDB rolled back as the victim of a deadlock. Nothing of the
// transaction was written, so running it again from the start is safe.
func IsDeadlock(err error) bool {
	var mysqlErr *mysql.MySQLError

	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlErrLockDeadlock
}

// IsDuplicateKeyNamed reports a write refused by one particular unique key, for a table with more than one. MySQL
// names the key at the end of the message ("Duplicate entry '7' for key 'orders.uq_number'", MariaDB without the
// table), so key is matched anywhere in it: pass the index name.
func IsDuplicateKeyNamed(err error, key string) bool {
	var mysqlErr *mysql.MySQLError

	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlErrDuplicateEntry && strings.Contains(mysqlErr.Message, key)
}
