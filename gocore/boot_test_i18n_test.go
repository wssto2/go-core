//nolint:testpackage // tests the unexported lifecycle and builder gocore is built on
package gocore

import "testing"

func tempI18nDir(t *testing.T) string {
	t.Helper()

	return t.TempDir()
}
