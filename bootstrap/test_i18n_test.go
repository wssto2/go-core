package bootstrap

import "testing"

func tempI18nDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}
