package archtest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/archtest"
)

const (
	mappingFile = "documentation/mapping.md"
	literalID   = "VEH-LITERAL-003"
	nowhereID   = "VEH-NOWHERE-004"
)

// logicIDModule is a scratch module with a registry of four rules: one anchored
// in a Go comment, one in a frontend file, one named only in a string literal,
// one named nowhere.
func logicIDModule(t *testing.T) (string, archtest.LogicIDConfig) {
	t.Helper()

	root := t.TempDir()

	write(t, root, mappingFile, "# Mapping\n\n"+
		"- VEH-CREATE-001 — a vehicle needs a VIN — internal/vehicle.go\n"+
		"- VEH-PRICE-002 — a price is positive — frontend/features/price.ts\n"+
		"- VEH-LITERAL-003 — only in a string\n"+
		"- VEH-NOWHERE-004 — nobody implements it\n"+
		"Not a line: VEH-PROSE-005 — mentioned in prose\n")
	write(t, root, "internal/vehicle.go", "package vehicle\n\n// Create (VEH-CREATE-001) needs a VIN.\n"+
		"func Create() {}\n\nvar _ = \""+literalID+"\"\n")
	write(t, root, "frontend/features/price.ts", "// VEH-PRICE-002 a price is positive\nexport const a = 1\n")
	write(t, root, "frontend/features/ignored.md", "VEH-NOWHERE-004\n")

	var cfg archtest.LogicIDConfig

	cfg.Root = root
	cfg.Mapping = mappingFile
	cfg.Go = []string{internalDir}
	cfg.Text = []string{"frontend/features"}
	cfg.TextExt = []string{".ts", ".vue"}

	return root, cfg
}

func TestLogicIDsReportsWhatIsNotAnchored(t *testing.T) {
	t.Parallel()

	_, cfg := logicIDModule(t)

	got, err := archtest.CheckLogicIDs(cfg)
	require.NoError(t, err)

	text := strings.Join(got, "\n")
	require.Contains(t, text, "UNANCHORED LOGIC IDs")
	require.Contains(t, text, literalID, "a string literal is not an anchor")
	require.Contains(t, text, nowhereID, "a text file of another extension is not an anchor")
	require.NotContains(t, text, "VEH-CREATE-001")
	require.NotContains(t, text, "VEH-PRICE-002")
	require.NotContains(t, text, "VEH-PROSE-005", "only a mapping line declares an ID")
	require.NotContains(t, text, "STALE")

	cfg.NotYetPorted = []string{literalID, nowhereID}

	got, err = archtest.CheckLogicIDs(cfg)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestLogicIDsNotYetPortedIsARatchet(t *testing.T) {
	t.Parallel()

	_, cfg := logicIDModule(t)
	cfg.NotYetPorted = []string{literalID, nowhereID, "VEH-CREATE-001"}

	got, err := archtest.CheckLogicIDs(cfg)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "STALE NotYetPorted")
	require.Contains(t, got[0], "VEH-CREATE-001")
	require.NotContains(t, got[0], nowhereID)
}

func TestLogicIDsHoldRuleFilesToTheMapping(t *testing.T) {
	t.Parallel()

	root, cfg := logicIDModule(t)
	cfg.NotYetPorted = []string{literalID, nowhereID}
	cfg.RuleFiles = []string{"documentation/rules/*.md", mappingFile}

	write(t, root, "documentation/rules/vehicle.md",
		"## VEH-CREATE-001 — create\n\n## VEH-NEW-009 — not in the registry\n")

	got, err := archtest.CheckLogicIDs(cfg)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Contains(t, got[0], "documentation/rules/vehicle.md")
	require.Contains(t, got[0], "VEH-NEW-009")
	require.NotContains(t, got[0], "VEH-CREATE-001")
}

func TestLogicIDsErrors(t *testing.T) {
	t.Parallel()

	root, cfg := logicIDModule(t)

	cfg.Mapping = "documentation/missing.md"
	_, err := archtest.CheckLogicIDs(cfg)
	require.ErrorContains(t, err, "documentation/missing.md")

	write(t, root, "documentation/empty.md", "no ids here\n")

	cfg.Mapping = "documentation/empty.md"
	_, err = archtest.CheckLogicIDs(cfg)
	require.ErrorContains(t, err, "no Logic IDs")

	_, cfg = logicIDModule(t)
	cfg.Go = []string{"internal/missing"}
	_, err = archtest.CheckLogicIDs(cfg)
	require.ErrorContains(t, err, "internal/missing")

	root, cfg = logicIDModule(t)
	write(t, root, "internal/broken.go", "package vehicle\nfunc (\n")

	_, err = archtest.CheckLogicIDs(cfg)
	require.ErrorContains(t, err, "internal/broken.go")
}
