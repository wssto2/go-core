//nolint:err113,exhaustruct,exhaustruct_v5,lll,noinlineerr,paralleltest,thelper,varnamelen // tests in the style of the package's others
package database_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/gorm"
)

func TestIsDuplicateKey_Messages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"mysql error", &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'a' for key 'PRIMARY'"}, true},
		{"mysql error wrapped", fmt.Errorf("create: %w", &mysql.MySQLError{Number: 1062}), true},
		{"mysql other number", &mysql.MySQLError{Number: 1213, Message: "Deadlock found"}, false},
		{"gorm translated", fmt.Errorf("create: %w", gorm.ErrDuplicatedKey), true},
		{"sqlite unique", errors.New("UNIQUE constraint failed: idempotency_records.key"), true},
		{"mysql text", errors.New("Error 1062: Duplicate entry 'abc' for key 'PRIMARY'"), true},
		{"postgres text", errors.New(`duplicate key value violates unique constraint "idx_key"`), true},
		{"the word unique", errors.New("this value must be unique in the set"), false},
		{"constraint failed alone", errors.New("constraint failed: some other reason"), false},
		{"unrelated", errors.New("connection refused"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, database.IsDuplicateKey(tc.err))
		})
	}
}

func TestIsDeadlock_ByNumber(t *testing.T) {
	assert.True(t, database.IsDeadlock(fmt.Errorf("x: %w", &mysql.MySQLError{Number: 1213})))
	assert.False(t, database.IsDeadlock(&mysql.MySQLError{Number: 1062}))
	assert.False(t, database.IsDeadlock(errors.New("Deadlock found")))
	assert.False(t, database.IsDeadlock(nil))
}

func TestIsDuplicateKeyNamed_ByNumberAndKey(t *testing.T) {
	err := fmt.Errorf("x: %w", &mysql.MySQLError{Number: 1062, Message: "Duplicate entry '7' for key 'orders.uq_number'"})

	assert.True(t, database.IsDuplicateKeyNamed(err, "uq_number"))
	assert.False(t, database.IsDuplicateKeyNamed(err, "uq_other"))
	assert.False(t, database.IsDuplicateKeyNamed(&mysql.MySQLError{Number: 1213, Message: "uq_number"}, "uq_number"))
	assert.False(t, database.IsDuplicateKeyNamed(errors.New("Duplicate entry '7' for key 'uq_number'"), "uq_number"))
}

type uniqueRow struct {
	ID   int    `gorm:"primaryKey;autoIncrement"`
	Code string `gorm:"size:20;uniqueIndex:uq_unique_rows_code"`
	Slot string `gorm:"size:20;uniqueIndex:uq_unique_rows_slot"`
}

// The classifiers read the errors the servers really send, MySQL 9 and MariaDB 10.3 alike.
func TestMySQLErrors_AgainstTheServers(t *testing.T) {
	run := func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&uniqueRow{}))
		require.NoError(t, db.Create(&uniqueRow{Code: "a", Slot: "1"}).Error)

		byCode := db.Create(&uniqueRow{Code: "a", Slot: "2"}).Error
		bySlot := db.Create(&uniqueRow{Code: "b", Slot: "1"}).Error

		assert.True(t, database.IsDuplicateKey(byCode))
		assert.True(t, database.IsDuplicateKeyNamed(byCode, "uq_unique_rows_code"))
		assert.False(t, database.IsDuplicateKeyNamed(byCode, "uq_unique_rows_slot"))
		assert.True(t, database.IsDuplicateKeyNamed(bySlot, "uq_unique_rows_slot"))
		assert.False(t, database.IsDeadlock(byCode))
	}

	dbtest.Run(t, run, dbtest.MySQL, dbtest.MariaDB)
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&uniqueRow{}))
		require.NoError(t, db.Create(&uniqueRow{Code: "a", Slot: "1"}).Error)
		assert.True(t, database.IsDuplicateKey(db.Create(&uniqueRow{Code: "a", Slot: "2"}).Error))
	}, dbtest.SQLite)
}

type lockRow struct {
	ID int `gorm:"primaryKey"`
	N  int
}

// Two transactions that lock the two rows in opposite orders: InnoDB picks one as the victim.
func TestIsDeadlock_AgainstTheServers(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&lockRow{}))
		require.NoError(t, db.Create(&[]lockRow{{ID: 1}, {ID: 2}}).Error)

		var (
			wg   sync.WaitGroup
			errs = make(chan error, 2)
			held = [2]chan struct{}{make(chan struct{}), make(chan struct{})}
		)

		// lock takes row first, says so, waits until the other transaction holds its row, then asks for the other row.
		lock := func(me, first, second int) {
			defer wg.Done()

			errs <- database.NewTransactor(db).WithinTransaction(t.Context(), func(ctx context.Context) error {
				tx, _ := database.TxFromContext(ctx)
				if err := tx.Exec("UPDATE lock_rows SET n = n + 1 WHERE id = ?", first).Error; err != nil {
					return err
				}

				close(held[me])
				<-held[1-me]

				return tx.Exec("UPDATE lock_rows SET n = n + 1 WHERE id = ?", second).Error
			})
		}

		wg.Add(2)

		go lock(0, 1, 2)
		go lock(1, 2, 1)

		wg.Wait()
		close(errs)

		deadlocks := 0

		for err := range errs {
			if err != nil {
				require.True(t, database.IsDeadlock(err), "unexpected error: %v", err)

				deadlocks++
			}
		}

		assert.Equal(t, 1, deadlocks, "exactly one transaction is the victim")
	}, dbtest.MySQL, dbtest.MariaDB)
}
