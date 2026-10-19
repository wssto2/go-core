package database_test

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
)

var errGone = errors.New("gone")

func TestNotFoundAs(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom") //nolint:err113

	require.NoError(t, database.NotFoundAs(nil, errGone))
	require.Equal(t, errGone, database.NotFoundAs(gorm.ErrRecordNotFound, errGone))
	require.Equal(t, errGone, database.NotFoundAs(fmt.Errorf("find: %w", gorm.ErrRecordNotFound), errGone))
	require.Equal(t, errGone, database.NotFoundAs(sql.ErrNoRows, errGone))
	require.Equal(t, boom, database.NotFoundAs(boom, errGone))
}
