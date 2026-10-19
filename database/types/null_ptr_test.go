package types_test

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/types"
)

func TestNullPtrConstructorsNilIsNullAndAValueIsKept(t *testing.T) {
	t.Parallel()

	whole, fraction, day := 7, 2.5, time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		null, some driver.Valuer
		want       driver.Value
	}{
		"int":   {types.NewNullIntPtr(nil), types.NewNullIntPtr(&whole), int64(7)},
		"float": {types.NewNullFloatPtr(nil), types.NewNullFloatPtr(&fraction), 2.5},
		"date":  {types.NewNullDatePtr(nil), types.NewNullDatePtr(&day), "2026-03-04"},
	} {
		got, err := tc.null.Value()
		require.NoError(t, err, name)
		assert.Nil(t, got, name+": nil is NULL")

		got, err = tc.some.Value()
		require.NoError(t, err, name)
		assert.Equal(t, tc.want, got, name)
	}

	var zero time.Time

	got, err := types.NewNullDatePtr(&zero).Value()
	require.NoError(t, err)
	assert.Nil(t, got, "a zero time is NULL, like NewNullDate")
}
