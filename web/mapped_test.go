//nolint:varnamelen // tests in the style of the package's others
package web_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/web"
)

func TestMapPtrKeepsNilAndMapsARecordWithoutAliasingIt(t *testing.T) {
	t.Parallel()

	assert.Nil(t, web.MapPtr(nil, strconv.Itoa))

	n := 7
	got := web.MapPtr(&n, strconv.Itoa)

	require.NotNil(t, got)
	assert.Equal(t, "7", *got)

	n = 8

	assert.Equal(t, "7", *got, "the result is its own value")
}
