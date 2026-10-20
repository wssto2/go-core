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
