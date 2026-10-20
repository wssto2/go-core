package archtest_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/archtest"
)

// guardCase: the rule reports the module with the overlay's files added, naming
// every string of want and none of not.
type guardCase struct {
	rule    string
	overlay string
	want    []string
	not     []string
}

func runGuardCases(t *testing.T, cases []guardCase) {
	t.Helper()

	for _, one := range cases {
		t.Run(one.rule, func(t *testing.T) {
			t.Parallel()

			got := problems(t, testConfig(fixture(t, one.overlay)), one.rule)
			require.NotEmpty(t, got, "the guard caught nothing")

			for _, want := range one.want {
				require.Contains(t, got, want)
			}

			for _, not := range one.not {
				require.NotContains(t, got, not)
			}
		})
	}
}

func TestGuardCatchesConventions(t *testing.T) {
	t.Parallel()

	runGuardCases(t, []guardCase{
		{archtest.NoPanic, "no-panic", []string{orderDomain + "/p.go:3  panic("}, []string{"p_test.go"}},
		{archtest.NoContextBackground, "no-context-background", []string{orderApp + "/c.go:5"}, nil},
		{archtest.NoSleep, "no-sleep", []string{orderApp + "/s.go:5"}, nil},
		{archtest.NoDatabaseSQL, "no-database-sql", []string{orderDomain + "/sql.go:3  imports database/sql"}, nil},
		{archtest.ErrorfWrap, "errorf-wrap", []string{orderApp + "/e.go:5  fmt.Errorf(...) passes err without %w"}, nil},
		{
			archtest.HandlersNoPersistence, "handlers-no-persistence",
			[]string{"order/h.go:3  imports example.com/app/internal/infrastructure/db/repository"}, nil,
		},
	})
}

func TestGuardCatchesLayers(t *testing.T) {
	t.Parallel()

	runGuardCases(t, []guardCase{
		{
			archtest.LayerImports, "layer-imports",
			[]string{orderDomain + "/l.go:3", orderDomain + "/l_test.go:3", orderApp + "/l.go:3", orderApp + "/l_cmd.go:3"},
			[]string{"l_domain.go", "outward.go"},
		},
		{
			archtest.DomainPurity, "domain-purity",
			[]string{
				"frameworks.go:4  imports encoding/json", "frameworks.go:5  imports github.com/gin-gonic/gin",
				"frameworks.go:6  imports github.com/wssto2/go-core/datatable", "frameworks.go:7  imports gorm.io/gorm/clause",
				"tagged.go:4  has a json: tag",
			},
			[]string{"kernel.go", "other_tag.go"},
		},
		{
			archtest.ApplicationPurity, "application-purity",
			[]string{"frameworks.go:4  imports github.com/gin-gonic/gin", "frameworks.go:5  imports gorm.io/gorm"}, nil,
		},
		{archtest.NoTimeNowDomain, "no-time-now-domain", []string{"now.go:5", "now.go:7"}, nil},
		{archtest.NoTimeNowApplication, "no-time-now-application", []string{"now.go:5"}, nil},
		{
			archtest.CrossAggregate, "cross-aggregate",
			[]string{"reaches.go:3  imports the customer aggregate"}, []string{"kernel.go", "self.go", "top.go"},
		},
		{
			archtest.DomainContextFirst, "domain-context-first",
			[]string{"port.go:6  Port.Get does not take context.Context first"}, []string{"Port.OK"},
		},
		{
			archtest.ServiceContextFirst, "service-context-first",
			[]string{"offer.go:5  OfferAppService.Do does not take context.Context first"}, []string{"hidden", "Other"},
		},
	})
}

func TestGuardCatchesData(t *testing.T) {
	t.Parallel()

	runGuardCases(t, []guardCase{
		{
			archtest.BoundedStrings, "bounded-strings",
			[]string{"input.go:4  Update.Title (string) has no max:", "input.go:13  Place.Text (string) has no max:"},
			[]string{"Note", "Filter", "Tags"},
		},
		{archtest.ValidationRules, "validation-rules", []string{"rules.go:4", "gte=0"}, []string{"rules.go:5"}},
		{
			archtest.ServiceRepositories, "service-repositories",
			[]string{
				orderApp + "/injects.go injects a Repository from internal/domain/customer",
				orderApp + "/aliased.go injects a Repository",
			},
			[]string{"bare.go"},
		},
		{
			archtest.RepositoryShape, "repository-shape",
			[]string{"bad.go:9  uses db.TxFor", "bad.go:10  uses tenancy.ScopeByTenant"},
			[]string{"base.go", "scopes.go"},
		},
	})
}

func TestGuardCatchesRoutes(t *testing.T) {
	t.Parallel()

	runGuardCases(t, []guardCase{
		{
			archtest.RoutePaths, "route-paths",
			[]string{
				`segment "sendOrder"`, `segment "price_rules"`, `segment "Files"`, `segment "Step1"`,
				"not a string literal or constants",
			},
			[]string{`segment "1"`, `"files"`, `"path"`, `"not a route"`},
		},
		{
			archtest.RouteIdentifiers, "route-identifiers",
			[]string{
				`permission namespace component "sales_plan"`, `permission verb "view_all"`,
				`route group segment "sales-plan"`, `component "Y"`,
			},
			[]string{`"salesplan"`, `"ok"`, `"fine"`},
		},
		{
			archtest.RouteAccess, "route-access",
			[]string{"access.go:6  GET /v1/orders declares no access", "access.go:7  POST /v1/orders/raw declares no access"},
			[]string{"access.go:8", "access.go:9", "access.go:10", "access.go:11", "access.go:12"},
		},
	})
}
