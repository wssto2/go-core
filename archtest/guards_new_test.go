//nolint:exhaustruct,exhaustruct_v5 // fixtures set only the fields under test
package archtest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/archtest"
)

const (
	vehicleFile = "internal/domain/vehicle/vehicle.go"
	vehicleDir  = "internal/domain/vehicle"
	cmdDir      = "cmd"
	frameAuth   = "example.com/framework/auth"
	frameTenant = "example.com/framework/tenancy"
)

func TestFieldWritesOnlyInTheListedFunctions(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, "guards"))
	cfg.FieldWrites = []archtest.FieldWrites{{
		Type: "Workflow", Field: "Phase", Why: "go through transitionPhase",
		Only: []string{vehicleFile + ":transitionPhase"},
	}}

	got := problems(t, cfg, "field-writes")
	require.Contains(t, got, "FIELD WRITES (Workflow.Phase)")
	require.Contains(t, got, "vehicle.go:17  assigns Workflow.Phase", "Sneak")
	require.Contains(t, got, "vehicles/service.go:6  assigns Workflow.Phase", "another layer")
	require.NotContains(t, got, "vehicle.go:15", "transitionPhase is listed")
	require.Contains(t, got, "-> go through transitionPhase")

	cfg.FieldWrites[0].Only = []string{vehicleDir, "internal/application/vehicles/..."}
	require.Empty(t, problems(t, cfg, "field-writes"), "a place allows every function in it")
}

func TestFieldWritesAnyFieldAndIncrements(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, "guards"))
	cfg.FieldWrites = []archtest.FieldWrites{
		{Type: "Status", In: []string{"internal/application/vehicles/..."}, Why: "call the domain"},
		{Type: "Pricing", Field: "ListPrice", Only: []string{vehicleFile + ":ChangePrices"}, Why: "ChangePrices"},
	}

	got := problems(t, cfg, "field-writes")
	require.Contains(t, got, "service.go:7  assigns Status.Active")
	require.Contains(t, got, "vehicle.go:26  assigns Pricing.ListPrice", "ListPrice++")
	require.NotContains(t, got, "vehicle.go:20", "ChangePrices")
	require.NotContains(t, got, "Pricing.Note", "another field of the group")
}

func TestFieldWritesLiterals(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, "guards"))
	cfg.FieldWrites = []archtest.FieldWrites{{
		Type: "Pricing", Field: "ListPrice", Literal: true, Package: vehicleDir, Why: "ChangePrices",
		Only: []string{vehicleFile + ":ChangePrices"}, In: []string{"internal/domain/...", "internal/application/..."},
	}}

	got := problems(t, cfg, "field-writes")
	require.Contains(t, got, "vehicle.go:27  sets ListPrice in a Pricing literal")
	require.Contains(t, got, "vehicles/service.go:9  sets ListPrice in a Pricing literal", "through the import alias")
	require.NotContains(t, got, "vehicle.go:21", "ChangePrices")
	require.NotContains(t, got, "offer.go", "another package's Pricing")
}

func TestRequiresCall(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, "guards"))
	cfg.RequiresCall = []archtest.RequiresCall{{
		Call: frameAuth + ".Authenticated", Also: frameTenant + ".FromAuthenticatedUser", Why: "tenancy fails closed",
	}}

	got := problems(t, cfg, "requires-call")
	require.Contains(t, got, "REQUIRES CALL (auth.Authenticated needs tenancy.FromAuthenticatedUser)")
	require.Contains(t, got, "http/routes_b.go:5  calls auth.Authenticated without tenancy.FromAuthenticatedUser")
	require.NotContains(t, got, "routes_a.go")
	require.NotContains(t, got, "routes_c.go", "an alias is resolved by import path")
	require.Contains(t, got, "-> tenancy fails closed")

	cfg.RequiresCall[0].Except = []string{"internal/infrastructure/http/routes_b.go"}
	require.Empty(t, problems(t, cfg, "requires-call"))

	cfg.RequiresCall = []archtest.RequiresCall{{Call: "Authenticated", Also: frameTenant + ".X", Why: "x"}}
	_, err := archtest.Check(cfg)
	require.ErrorContains(t, err, "RequiresCall.Call")
}

func TestContextRootsAndWrappers(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, "guards"))
	cfg.ContextRoots = []archtest.ContextRoots{{
		Dirs: []string{cmdDir}, Why: "use SystemContext",
		Wrappers: []string{frameTenant + ".WithAllTenants", frameTenant + ".WithTenantID", ".InfoContext"},
	}}

	got := problems(t, cfg, "context-roots")
	require.Contains(t, got, "cmd/app/roots.go:13  context.Background() as a root context")
	require.Contains(t, got, "cmd/app/roots.go:14  context.TODO() as a root context")
	require.NotContains(t, got, "roots.go:11", "a tenancy wrapper")
	require.NotContains(t, got, "roots.go:12", "a logger's *Context method")
	require.NotContains(t, got, "roots.go:15", "TODO as the wrapped argument")
	require.Contains(t, got, "-> use SystemContext")

	cfg.ContextRoots[0].Wrappers = nil
	require.Contains(t, problems(t, cfg, "context-roots"), "roots.go:11")
}

func TestNoContextBackgroundAlsoBansTODO(t *testing.T) {
	t.Parallel()

	got := problems(t, testConfig(fixture(t, "guards")), archtest.NoContextBackground)
	require.Contains(t, got, "order/todo.go:5")
}

func TestRepositoryBansANestedTransaction(t *testing.T) {
	t.Parallel()

	cfg := testConfig(fixture(t, "guards"))
	require.Empty(t, problems(t, cfg, archtest.RepositoryShape), "off unless asked")

	cfg.Repository.NoNestedTransaction = true

	got := problems(t, cfg, archtest.RepositoryShape)
	require.Contains(t, got, "repository/tx.go:8")
	require.Contains(t, got, "WithinTx")
	require.NotContains(t, got, "tx.go:12", "WithinTx is the way")
}

func TestRepositoryHandleFileAndNoNilNil(t *testing.T) {
	t.Parallel()

	root := fixture(t, "repository-handle")
	cfg := testConfig(root)
	require.Empty(t, problems(t, cfg, archtest.RepositoryShape), "off unless asked")

	cfg.Repository.HandleFile = "handle_base.go"
	cfg.Repository.NoNilNil = true

	got := problems(t, cfg, archtest.RepositoryShape)
	require.Contains(t, got, "repository/handle_bad.go:5  hand-written db method")
	require.Contains(t, got, "repository/handle_bad.go:8  nilnil suppressed")
	require.NotContains(t, got, "handle_base.go:", "the handle file declares DB")
	require.NotContains(t, got, "handle_bad.go:12", "another linter's nolint")

	cfg.Repository.NoNilNil = false
	require.NotContains(t, problems(t, cfg, archtest.RepositoryShape), "nilnil suppressed", "each part is its own switch")

	require.NoError(t, os.Remove(filepath.Join(root, "internal/infrastructure/db/repository/handle_bad.go")))

	cfg.Repository.NoNilNil = true
	require.Empty(t, problems(t, cfg, archtest.RepositoryShape), "without the planted file the rule holds")
}

func TestScopedQueriesStartFromTheScopedHandle(t *testing.T) {
	t.Parallel()

	root := fixture(t, "scoped-queries")
	cfg := testConfig(root)
	cfg.ScopedQueries = []archtest.ScopedQueries{{
		Files: []string{"internal/infrastructure/db/repository/vehicle_read.go"},
		Call:  "Model", Arg: "gen.Vehicle", Via: []string{"scopedDB", "crossTenantDB"}, Why: "start from scopedDB",
	}}

	got := problems(t, cfg, "scoped-queries")
	require.Contains(t, got, "SCOPED QUERIES (Model(&gen.Vehicle{}))")
	require.Contains(t, got, "vehicle_read.go:14  Model(&gen.Vehicle{}) starts from db, not scopedDB or crossTenantDB")
	require.Contains(t, got, "vehicle_read.go:15", "a longer chain")
	require.Contains(t, got, "vehicle_read.go:20  Model(&gen.Vehicle{}) starts from something that is not a call")
	require.NotContains(t, got, "vehicle_read.go:8", "scopedDB")
	require.NotContains(t, got, "vehicle_read.go:9", "crossTenantDB")
	require.NotContains(t, got, "vehicle_read.go:10", "another model")
	require.NotContains(t, got, "other.go", "a file that is not listed")
	require.Contains(t, got, "-> start from scopedDB")

	cfg.ScopedQueries[0].Files = []string{"internal/infrastructure/db/repository/nowhere.go"}
	_, err := archtest.Check(cfg)
	require.ErrorContains(t, err, "ScopedQueries.Files")

	require.NoError(t, os.Remove(filepath.Join(root, "internal/infrastructure/db/repository/vehicle_read.go")))
	cfg.ScopedQueries[0].Files = []string{"internal/infrastructure/db/repository/other.go"}
	require.Contains(t, problems(t, cfg, "scoped-queries"), "other.go:5")

	cfg.ScopedQueries[0].Via = append(cfg.ScopedQueries[0].Via, "db")
	require.Empty(t, problems(t, cfg, "scoped-queries"), "with the handle allowed the rule holds")
}
