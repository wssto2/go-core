package archtest_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/archtest"
)

const (
	vehicleFile = "internal/domain/vehicle/vehicle.go"
	vehicleDir  = "internal/domain/vehicle"
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
	cfg.RequiresCall = []archtest.RequiresCall{{Call: frameAuth + ".Authenticated", Also: frameTenant + ".FromAuthenticatedUser", Why: "tenancy fails closed"}}

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
		Dirs: []string{"cmd"}, Why: "use SystemContext",
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
