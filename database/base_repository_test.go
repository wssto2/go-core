//nolint:err113,exhaustruct,exhaustruct_v5,lll,paralleltest,thelper,varnamelen // tests in the style of the package's others
package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/gorm"
)

type txNote struct {
	ID   int `gorm:"primaryKey;autoIncrement"`
	Text string
}

func countNotes(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var n int64

	require.NoError(t, db.Model(&txNote{}).Count(&n).Error)

	return n
}

func TestBaseRepository_WithinTx(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&txNote{}))

		repo := &database.BaseRepository{Conn: db}
		ctx := t.Context()

		assert.False(t, repo.InTx(ctx))

		t.Run("commits when fn returns nil and fn sees the transaction", func(t *testing.T) {
			err := repo.WithinTx(ctx, func(ctx context.Context) error {
				assert.True(t, repo.InTx(ctx))

				return repo.DB(ctx).Create(&txNote{Text: "kept"}).Error
			})

			require.NoError(t, err)
			assert.EqualValues(t, 1, countNotes(t, db))
		})

		t.Run("rolls back when fn returns an error", func(t *testing.T) {
			boom := errors.New("boom")
			err := repo.WithinTx(ctx, func(ctx context.Context) error {
				require.NoError(t, repo.DB(ctx).Create(&txNote{Text: "lost"}).Error)

				return boom
			})

			require.ErrorIs(t, err, boom)
			assert.EqualValues(t, 1, countNotes(t, db))
		})

		t.Run("a nested call joins the outer transaction", func(t *testing.T) {
			boom := errors.New("boom")
			err := repo.WithinTx(ctx, func(outer context.Context) error {
				inner := repo.WithinTx(outer, func(inner context.Context) error {
					return repo.DB(inner).Create(&txNote{Text: "inner"}).Error
				})
				require.NoError(t, inner)

				// the inner call did not commit: the outer one still decides
				return boom
			})

			require.ErrorIs(t, err, boom)
			assert.EqualValues(t, 1, countNotes(t, db))
		})
	})
}

func TestPlainRepository_IgnoresTheTransactionInContext(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&txNote{}))

		base := &database.BaseRepository{Conn: db}
		plain := &database.PlainRepository{Conn: db}

		err := base.WithinTx(t.Context(), func(ctx context.Context) error {
			tx, ok := database.TxFromContext(ctx)
			require.True(t, ok)

			assert.Same(t, tx.Statement.ConnPool, base.DB(ctx).Statement.ConnPool, "the base repository uses the transaction")
			assert.NotSame(t, tx.Statement.ConnPool, plain.DB(ctx).Statement.ConnPool, "the plain one does not")

			return nil
		})

		require.NoError(t, err)
	})
}

// The write of a PlainRepository commits on its own connection, so it survives the rollback of the transaction around
// it. SQLite is left out: its in-memory database has one connection, which the open transaction holds.
func TestPlainRepository_WriteSurvivesTheCallersRollback(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&txNote{}))

		base := &database.BaseRepository{Conn: db}
		plain := &database.PlainRepository{Conn: db}
		boom := errors.New("boom")

		err := base.WithinTx(t.Context(), func(ctx context.Context) error {
			require.NoError(t, base.DB(ctx).Create(&txNote{Text: "rolled back"}).Error)
			require.NoError(t, plain.DB(ctx).Create(&txNote{Text: "kept"}).Error)

			return boom
		})

		require.ErrorIs(t, err, boom)

		var rows []txNote

		require.NoError(t, db.Find(&rows).Error)
		require.Len(t, rows, 1)
		assert.Equal(t, "kept", rows[0].Text)
	}, dbtest.MySQL, dbtest.MariaDB)
}
