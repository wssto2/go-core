package types

import (
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntType_Value_ReturnsInt64(t *testing.T) {
	v := NewInt(42)
	val, err := v.Value()
	require.NoError(t, err)
	_, ok := val.(int64)
	assert.True(t, ok, "Int.Value() must return driver.Value of type int64, got %T", val)
	assert.Equal(t, int64(42), val)
}

func TestNullIntType_Value_ReturnsInt64(t *testing.T) {
	v := NewNullInt(99)
	val, err := v.Value()
	require.NoError(t, err)
	_, ok := val.(int64)
	assert.True(t, ok, "NullInt.Value() must return driver.Value of type int64, got %T", val)
	assert.Equal(t, int64(99), val)
}

func TestNullIntType_NilValue_ReturnsNil(t *testing.T) {
	var v NullInt // nil pointer
	val, err := v.Value()
	require.NoError(t, err)
	assert.Equal(t, driver.Value(nil), val)
}
