//nolint:exhaustruct,exhaustruct_v5 // fixtures set only the fields under test
package archtest_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/archtest"
)

func auditConfig(t *testing.T) archtest.Config {
	t.Helper()

	cfg := testConfig(fixture(t, "audit"))
	writes := archtest.ServiceWrites{
		Dirs: []string{"internal/application/ledger"}, Repositories: []string{"internal/domain"},
	}
	cfg.RequiresAudit = []archtest.RequiresAudit{{Writes: writes}}
	cfg.AtomicWrites = []archtest.AtomicWrites{{Writes: writes}}

	return cfg
}

func TestRequiresAuditNamesTheMethodsThatWriteWithoutAnEntry(t *testing.T) {
	t.Parallel()

	got := problems(t, auditConfig(t), "requires-audit")
	require.Contains(t, got, "ledger.Service.DirectNoAudit writes (entries.Create at ")
	require.Contains(t, got, "ledger.Service.ViaHelperNoAudit", "through a same-package helper")
	require.Contains(t, got, "ledger.Service.InTxNoAudit", "inside a transaction closure")
	require.Contains(t, got, "ledger.Service.Exempted")
	require.Contains(t, got, "add it to RequiresAudit.Exempt")

	fines := []string{
		"Read ", "DirectAudited", "AuditedByWrite", "ViaHelperAudited", "InTxAudited", "TwoRepos", "OneRepoTwice",
	}
	for _, fine := range fines {
		require.NotContains(t, got, "ledger.Service."+fine)
	}
}

func TestRequiresAuditExemptions(t *testing.T) {
	t.Parallel()

	cfg := auditConfig(t)
	cfg.RequiresAudit[0].Exempt = []archtest.AuditExemption{
		{Method: "ledger.Service.Exempted", Why: "a job table records it"},
	}

	got := problems(t, cfg, "requires-audit")
	require.NotContains(t, got, "Service.Exempted")
	require.Contains(t, got, "Service.DirectNoAudit")

	cfg.RequiresAudit[0].Exempt = append(cfg.RequiresAudit[0].Exempt,
		archtest.AuditExemption{Method: "ledger.Service.Read", Why: "reads only"},
		archtest.AuditExemption{Method: "ledger.Service.DirectAudited", Why: "audited already"})

	got = problems(t, cfg, "requires-audit")
	require.Contains(t, got, "STALE AUDIT EXEMPTION:\n  ledger.Service.Read\n  ledger.Service.DirectAudited")
}

func TestRequiresAuditExemptionNeedsAWhy(t *testing.T) {
	t.Parallel()

	cfg := auditConfig(t)
	cfg.RequiresAudit[0].Exempt = []archtest.AuditExemption{{Method: "ledger.Service.Exempted", Why: " "}}

	_, err := archtest.Check(cfg)
	require.ErrorContains(t, err, "needs a Method and a Why")
}

func TestRequiresAuditCustomAuditCalls(t *testing.T) {
	t.Parallel()

	cfg := auditConfig(t)
	cfg.RequiresAudit[0].Audit = []string{".Write"}

	got := problems(t, cfg, "requires-audit")
	require.Contains(t, got, "Service.DirectAudited", "Recorder.Record no longer counts")
	require.NotContains(t, got, "Service.AuditedByWrite")
}

func TestAtomicWritesNeedsATransactionForTwoRepositories(t *testing.T) {
	t.Parallel()

	got := problems(t, auditConfig(t), "atomic-writes")
	require.Contains(t, got, "ledger.Service.TwoReposOutsideTx writes through 2 repositories")
	require.Contains(t, got, "ledger.Service.TwoReposViaHelperOutsideTx")
	require.NotContains(t, got, "TwoReposInTx")
	require.NotContains(t, got, "TwoReposViaHelperInTx", "a helper called in the closure is inside it")
	require.NotContains(t, got, "OneRepoTwice", "one repository needs no transaction")
	require.NotContains(t, got, "DirectNoAudit")

	cfg := auditConfig(t)
	cfg.AtomicWrites[0].Tx = []string{"Elsewhere"}
	require.Contains(t, problems(t, cfg, "atomic-writes"), "ledger.Service.TwoReposInTx")
}

func TestServiceRulesSeeEmbeddedFieldsAndLocalRepositories(t *testing.T) {
	t.Parallel()

	cfg := auditConfig(t)
	cfg.RequiresAudit[0].Writes.RepositoryNames = []string{"Repository", "Store"}

	got := problems(t, cfg, "requires-audit")
	require.Contains(t, got, "ledger.Service.EmbeddedNoAudit", "a field of an embedded struct")
	require.Contains(t, got, "ledger.Service.LocalNoAudit", "an interface of the package itself")
}

const legacyFee = "pricing.Offer.LegacyFee"

func floatMoneyConfig(t *testing.T) archtest.Config {
	t.Helper()

	cfg := testConfig(fixture(t, "floatmoney"))
	cfg.NoFloatMoney = []archtest.NoFloatMoney{{Dirs: []string{"internal/application/pricing"}}}

	return cfg
}

func TestNoFloatMoneyNamesTheFloatsWithAMoneyName(t *testing.T) {
	t.Parallel()

	got := problems(t, floatMoneyConfig(t), "no-float-money")
	for _, want := range []string{
		"pricing.go:4  pricing.Offer.NetPrice is a float64 field",
		"pricing.Offer.UnitPrice", "pricing.Offer.Totals", "pricing.Offer.VATAmount", legacyFee,
		"pricing.Calculator.Quote.basePrice is a float64 parameter",
		"pricing.Calculator.Quote.grossTotal is a float64 result",
		"pricing.Round.amount", "pricing.Offer.Apply.discount", "pricing.Mean.values",
		"use money.Money", "money.Rate",
	} {
		require.Contains(t, got, want)
	}

	for _, fine := range []string{
		"Network", "Valuer", "Percent", "NetworkID", "Offer.Cents", "Discounted", "Round.step", "Apply.rate",
	} {
		require.NotContains(t, got, fine)
	}
}

func TestNoFloatMoneyExemptions(t *testing.T) {
	t.Parallel()

	cfg := floatMoneyConfig(t)
	cfg.NoFloatMoney[0].Exempt = []archtest.FloatMoneyExemption{{Field: legacyFee, Why: "a wire format"}}

	got := problems(t, cfg, "no-float-money")
	require.NotContains(t, got, "Offer.LegacyFee")
	require.NotContains(t, got, "STALE")

	cfg.NoFloatMoney[0].Exempt = append(cfg.NoFloatMoney[0].Exempt,
		archtest.FloatMoneyExemption{Field: "pricing.Offer.Network", Why: "not money"})

	got = problems(t, cfg, "no-float-money")
	require.Contains(t, got, "STALE FLOAT MONEY EXEMPTION:\n  pricing.Offer.Network")
}

func TestNoFloatMoneyExemptionNeedsAWhy(t *testing.T) {
	t.Parallel()

	cfg := floatMoneyConfig(t)
	cfg.NoFloatMoney[0].Exempt = []archtest.FloatMoneyExemption{{Field: legacyFee, Why: " "}}

	_, err := archtest.Check(cfg)
	require.ErrorContains(t, err, "needs a Field and a Why")
}

func TestNoFloatMoneyCustomNamesAndDirs(t *testing.T) {
	t.Parallel()

	cfg := floatMoneyConfig(t)
	cfg.NoFloatMoney[0].Names = []string{"Network"}

	got := problems(t, cfg, "no-float-money")
	require.Contains(t, got, "pricing.Offer.Network")
	require.NotContains(t, got, "NetPrice")

	cfg.NoFloatMoney[0].Dirs = []string{"internal/infrastructure"}
	got = problems(t, cfg, "no-float-money")
	require.Empty(t, got)
}
