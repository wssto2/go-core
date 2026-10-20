package ledger

import (
	"context"

	"example.com/app/internal/domain/ledger"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/database"
)

type Service struct {
	entries  ledger.Repository
	balances ledger.BalanceRepository
	audit    audit.Repository
	rec      *audit.Recorder
	tx       database.Transactor
	deps
}

func (s *Service) Read(ctx context.Context) error {
	_, err := s.entries.Find(ctx, 1)
	return err
}

func (s *Service) DirectNoAudit(ctx context.Context) error {
	return s.entries.Create(ctx, &ledger.Entry{})
}

func (s *Service) DirectAudited(ctx context.Context) error {
	if err := s.entries.Create(ctx, &ledger.Entry{}); err != nil {
		return err
	}
	return s.rec.Record(ctx, "Entry", 1, "create", nil, nil)
}

func (s *Service) AuditedByWrite(ctx context.Context) error {
	if err := s.entries.Delete(ctx, 1); err != nil {
		return err
	}
	return s.audit.Write(ctx, audit.NewEntry("Entry", 1, 1, "delete"))
}

func (s *Service) ViaHelperNoAudit(ctx context.Context) error { return s.store(ctx) }

func (s *Service) ViaHelperAudited(ctx context.Context) error {
	if err := s.store(ctx); err != nil {
		return err
	}
	return s.record(ctx)
}

func (s *Service) InTxAudited(ctx context.Context) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.entries.Create(ctx, &ledger.Entry{}); err != nil {
			return err
		}
		return s.record(ctx)
	})
}

func (s *Service) InTxNoAudit(ctx context.Context) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.entries.Delete(ctx, 1)
	})
}

func (s *Service) Exempted(ctx context.Context) error { return s.entries.Delete(ctx, 2) }

func (s *Service) TwoReposOutsideTx(ctx context.Context) error {
	if err := s.entries.Create(ctx, &ledger.Entry{}); err != nil {
		return err
	}
	if err := s.balances.UpdateBalance(ctx, 1); err != nil {
		return err
	}
	return s.record(ctx)
}

func (s *Service) TwoReposInTx(ctx context.Context) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.entries.Create(ctx, &ledger.Entry{}); err != nil {
			return err
		}
		if err := s.balances.UpdateBalance(ctx, 1); err != nil {
			return err
		}
		return s.record(ctx)
	})
}

func (s *Service) TwoReposViaHelperInTx(ctx context.Context) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.both(ctx); err != nil {
			return err
		}
		return s.record(ctx)
	})
}

func (s *Service) TwoReposViaHelperOutsideTx(ctx context.Context) error {
	if err := s.both(ctx); err != nil {
		return err
	}
	return s.record(ctx)
}

func (s *Service) OneRepoTwice(ctx context.Context) error {
	if err := s.entries.Create(ctx, &ledger.Entry{}); err != nil {
		return err
	}
	if err := s.entries.Delete(ctx, 1); err != nil {
		return err
	}
	return s.record(ctx)
}

func (s *Service) store(ctx context.Context) error { return s.entries.Create(ctx, &ledger.Entry{}) }

func (s *Service) both(ctx context.Context) error {
	if err := s.entries.Create(ctx, &ledger.Entry{}); err != nil {
		return err
	}
	return s.balances.UpdateBalance(ctx, 1)
}

func (s *Service) record(ctx context.Context) error {
	return s.rec.Record(ctx, "Entry", 1, "x", nil, nil)
}

// Store is a port of the package itself.
type Store interface {
	Save(ctx context.Context) error
}

type deps struct {
	local Store
	extra ledger.Repository
}

func (s *Service) EmbeddedNoAudit(ctx context.Context) error {
	return s.extra.Create(ctx, &ledger.Entry{})
}

func (s *Service) LocalNoAudit(ctx context.Context) error { return s.local.Save(ctx) }
