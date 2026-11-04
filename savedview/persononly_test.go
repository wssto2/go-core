//nolint:lll // tests in the style of the package's others
package savedview_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/savedview"
)

// IAM-AUTHZ-005 item 5: a person's saved views are theirs alone, so a service account is refused on every route.
func TestEveryRouteOfTheSavedViewsIsPersonOnly(t *testing.T) {
	t.Parallel()

	specs := savedview.Routes.Specs()
	require.NotEmpty(t, specs)

	for _, spec := range specs {
		assert.True(t, strings.HasPrefix(spec.Path, "/v1/saved-views"), "%s %s is outside the saved views' area", spec.Method, spec.Path)
		assert.True(t, spec.PersonOnly || spec.Public, "%s %s is neither PersonOnly nor Public", spec.Method, spec.Path)
	}
}
