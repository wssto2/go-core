package archtest_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	authImport = "github.com/wssto2/go-core/auth"
	bFile      = "internal/application/order/b.go"
)

func TestBannedImport(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, "banned"))

	got := problems(t, cfg, "banned")
	require.Contains(t, got, "BANNED (example.com/legacy)")
	require.Contains(t, got, "b.go:4  imports example.com/legacy")
	require.Contains(t, got, "-> gone")
	require.NotContains(t, got, "b_test.go", "production code only")

	cfg.Banned[0].Tests = true
	require.Contains(t, problems(t, cfg, "banned"), "b_test.go:3")
}

func TestBannedSymbol(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, "banned"))
	cfg.Banned = append(cfg.Banned[:0], banned(authImport, "Authorize", "the gate is gone"))

	got := problems(t, cfg, "banned")
	require.Contains(t, got, "BANNED (github.com/wssto2/go-core/auth.Authorize)")
	require.Contains(t, got, "b.go:9  uses auth.Authorize")
	require.NotContains(t, got, "auth.User")
}

func TestBannedPlaces(t *testing.T) {
	t.Parallel()

	root := fixture(t, "banned")

	for place, covers := range map[string]bool{
		bFile:                                  true,
		"internal/domain/...":                  false,
		"internal/application/...":             true,
		orderApp:                               true,
		"internal/application":                 false, // a bare directory is that package only
		"internal/application/foo":             false,
		"internal/application/order/b_test.go": false,
	} {
		limited := testConfig(root)
		limited.Banned[0].In = []string{place}

		except := testConfig(root)
		except.Banned[0].Except = []string{place}

		require.Equal(t, covers, problems(t, limited, "banned") != "", "In %s", place)
		require.Equal(t, !covers, problems(t, except, "banned") != "", "Except %s", place)
	}
}

func TestBannedEverythingOutsideTheStandardLibraryUnless(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, "banned"))
	cfg.Banned = append(cfg.Banned[:0], banned("...", "", "closed"))
	cfg.Banned[0].Unless = []string{"github.com/wssto2/go-core/..."}
	cfg.Banned[0].In = []string{bFile}

	got := problems(t, cfg, "banned")
	require.Contains(t, got, "imports example.com/legacy")
	require.NotContains(t, got, "go-core/auth")
}
