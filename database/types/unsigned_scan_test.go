package types_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wssto2/go-core/database/types"
)

// go-sql-driver >= 1.8 returns BIGINT UNSIGNED as uint64 over the text protocol (a query with no bound arguments).
func TestScanAcceptsUint64(t *testing.T) {
	t.Parallel()

	var i types.Int
	require.NoError(t, i.Scan(uint64(42)))
	assert.Equal(t, 42, i.Get())
	require.ErrorIs(t, i.Scan(uint64(math.MaxUint64)), types.ErrIntOverflow)

	var ni types.NullInt
	require.NoError(t, ni.Scan(uint64(7)))
	assert.Equal(t, 7, ni.GetOrEmpty())
	require.ErrorIs(t, ni.Scan(uint64(math.MaxUint64)), types.ErrIntOverflow)

	var b types.Bool
	require.NoError(t, b.Scan(uint64(1)))
	assert.True(t, b.Get())

	var nb types.NullBool
	require.NoError(t, nb.Scan(uint64(0)))
	require.NotNil(t, nb.Get())
	assert.False(t, nb.GetOrEmpty())

	var f types.Float
	require.NoError(t, f.Scan(uint64(3)))
	assert.InDelta(t, 3.0, f.Get(), 0.001)

	var nf types.NullFloat
	require.NoError(t, nf.Scan(uint64(4)))
	assert.InDelta(t, 4.0, nf.GetOrEmpty(), 0.001)

	var e types.LegacyBoolEnum
	require.NoError(t, e.Scan(uint64(1)))
	assert.True(t, e.Get())
}
