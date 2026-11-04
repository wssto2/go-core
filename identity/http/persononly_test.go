//nolint:lll // tests in the style of the package's others
package http_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityhttp "github.com/wssto2/go-core/identity/http"
)

// IAM-AUTHZ-005 item 5: a service account never reaches what only a person has. Every route of identity's person areas
// (sign-in and session under /v1/auth, the profile under /v1/iam/profile) is PersonOnly, or public (sign-in has nobody
// to refuse yet). The people-admin routes under /v1/iam/users are permission-gated, not a person's own.
func TestEveryRouteOfAPersonAreaIsPersonOnly(t *testing.T) {
	t.Parallel()

	areas := []string{"/v1/auth", "/v1/iam/profile"}
	served := map[string]bool{}

	for _, spec := range identityhttp.Routes.Specs() {
		for _, area := range areas {
			if spec.Path != area && !strings.HasPrefix(spec.Path, area+"/") {
				continue
			}

			served[area] = true

			assert.True(t, spec.PersonOnly || spec.Public, "%s %s is in a person area but is neither PersonOnly nor Public", spec.Method, spec.Path)
		}
	}

	for _, area := range areas {
		require.True(t, served[area], "no identity route is in %q: the area moved, so this test must follow it", area)
	}
}
