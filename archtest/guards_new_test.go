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

func TestWriteOnlyFieldsAreWrittenNeverRead(t *testing.T) {
	t.Parallel()

	root := fixture(t, "write-only")
	cfg := testConfig(root)
	cfg.WriteOnly = []archtest.WriteOnly{{
		Type: "User", Fields: []string{"AccountTypeID", "AccountRole"}, Why: "decide on a permission",
		Only: []string{"internal/tool"},
	}}

	got := problems(t, cfg, "write-only")
	require.Contains(t, got, "WRITE ONLY (User)")

	for _, line := range []string{"11", "13", "15", "16"} {
		require.Contains(t, got, "application/user.go:"+line+"  reads", "line "+line)
	}

	require.Contains(t, got, "user.go:16  reads AccountRole", "a read through the group")
	require.NotContains(t, got, "user.go:17", "an assignment target")
	require.NotContains(t, got, "user.go:18", "on the path to an assignment target")
	require.NotContains(t, got, "user.go:19", "a literal key")
	require.NotContains(t, got, "user.go:21", "a package's name")
	require.NotContains(t, got, "tool/migrate.go", "Only may read")
	require.Contains(t, got, "-> decide on a permission")

	cfg.WriteOnly[0].Only = nil
	require.Contains(t, problems(t, cfg, "write-only"), "tool/migrate.go:3  reads AccountTypeID")

	require.NoError(t, os.Remove(filepath.Join(root, "internal/application/user.go")))
	require.NoError(t, os.Remove(filepath.Join(root, "internal/tool/migrate.go")))
	require.Empty(t, problems(t, cfg, "write-only"), "without the planted reads the rule holds")
}

// A removed helper is a Banned Method when it is called on a value, but a bare function name has no
// package to find it by: Banned cannot ban it.
func TestBannedNamesARemovedPredicateOnAValueOnly(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/app\n\ngo 1.26\n")
	write(t, root, "a/a.go", "package a\n\nimport \"fmt\"\n\nfunc f(u fmt.Stringer) { _ = u.IsWebmaster(); isStaff() }\n\nfunc isStaff() {}\n")

	cfg := testConfig(root)
	cfg.Layers = archtest.Layers{}
	cfg.Repository = archtest.Repository{}
	cfg.Banned = []archtest.Banned{
		{Import: "fmt", Method: "IsWebmaster", Why: "gone"},
		{Import: "fmt", Method: "isStaff", Why: "gone"},
	}

	got := problems(t, cfg, "banned")
	require.Contains(t, got, "a/a.go:5  uses .IsWebmaster")
	require.NotContains(t, got, "isStaff", "a bare function is not a method")
}

func TestPermissionCatalogueIsTheOnlySource(t *testing.T) {
	t.Parallel()

	root := fixture(t, "permission-catalogue")
	cfg := testConfig(root)
	cfg.PermissionCatalogue = archtest.PermissionCatalogue{
		IDs:          func() []string { return []string{"crm.lead:view", "crm.lead:manage"} },
		Constructors: []string{"domainpermission.Of"},
		Package:      "internal/iam/permission",
		Why:          "add it to the catalogue",
	}

	got := problems(t, cfg, "permission-catalogue")
	require.Contains(t, got, "PERMISSION CATALOGUE")
	require.Contains(t, got, `application/use.go:14  "crm.lead:erase" is not in`)
	require.Contains(t, got, `application/use.go:15  "crm.ghost:view" is not in`)
	require.NotContains(t, got, "use.go:13", "a catalogue entry")
	require.NotContains(t, got, "use.go:16", "a colon-shaped string of no module the catalogue knows")
	require.NotContains(t, got, "use.go:17")
	require.NotContains(t, got, "use.go:18", "Of with an entry")
	require.Contains(t, got, `use.go:19  "crm.lead:erase" (built by a constructor)`, "a constant of the same package")
	require.Contains(t, got, `use.go:20  "crm.lead:export" (built by a constructor)`, "a constant of an imported package")
	require.NotContains(t, got, "use.go:21", "a constant defined from another package's constant, and in the catalogue")
	require.NotContains(t, got, "use.go:22", "an argument that is not a constant")
	require.NotContains(t, got, "iam/permission/permission.go", "the catalogue's own package is exempt")
	require.Contains(t, got, "-> add it to the catalogue")

	cfg.PermissionCatalogue.Constructors = []string{"example.com/app/internal/iam/permission.Of"}
	require.Contains(t, problems(t, cfg, "permission-catalogue"), "use.go:19", "a constructor by import path")

	cfg.PermissionCatalogue.Package = ""
	require.Contains(t, problems(t, cfg, "permission-catalogue"), "permission.go:8", "the exemption is configurable")

	cfg.PermissionCatalogue.IDs = func() []string { return nil }
	_, err := archtest.Check(cfg)
	require.ErrorContains(t, err, "PermissionCatalogue.IDs")

	cfg.PermissionCatalogue.IDs = func() []string {
		return []string{"crm.lead:view", "crm.lead:manage", "crm.lead:erase", "crm.lead:export", "crm.ghost:view"}
	}
	cfg.PermissionCatalogue.Package = "internal/iam/permission"
	require.Empty(t, problems(t, cfg, "permission-catalogue"), "with every name in the catalogue the rule holds")
}
