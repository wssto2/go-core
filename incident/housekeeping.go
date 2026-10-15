package incident

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/incident/core"
)

// DigestRetention is how long an incident that went out in a digest stays in the queue's table before it is deleted.
const DigestRetention = 24 * time.Hour

const (
	housekeepingInterval = time.Hour
	pruneBatch           = 500
)

// Swept is what one sweep of the housekeeping deleted.
type Swept struct {
	Occurrences int
	Digest      int
}

// housekeeper removes what the module no longer needs: occurrences older than the retention, except the newest of
// each incident (the incident screen always has one to show), and the digest items that were sent. It is the worker
// Install starts, every hour; Incidents.Sweep runs it once.
type housekeeper struct {
	store  core.Store
	retain time.Duration
	clock  gocore.Clock
	log    *slog.Logger
}

// Name identifies the worker in logs and metrics.
func (housekeeper) Name() string { return "incident.housekeeping" }

// Run sweeps once at start and then every hour, until ctx is cancelled.
func (h housekeeper) Run(ctx context.Context) error {
	ticker := time.NewTicker(housekeepingInterval)
	defer ticker.Stop()

	for {
		swept, err := h.sweep(ctx)

		switch {
		case err != nil && ctx.Err() == nil:
			h.log.ErrorContext(ctx, "incident: housekeeping failed", "error", err)
		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		case swept != Swept{}:
			h.log.InfoContext(ctx, "incident: deleted old rows", "occurrences", swept.Occurrences, "digest", swept.Digest)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// sweep deletes in batches of pruneBatch until a batch is not full.
func (h housekeeper) sweep(ctx context.Context) (Swept, error) {
	var swept Swept

	now := h.clock.Now()

	for {
		deleted, err := h.store.PruneOccurrences(ctx, now.Add(-h.retain), pruneBatch)
		if err != nil {
			return swept, fmt.Errorf("incident housekeeping: %w", err)
		}

		swept.Occurrences += deleted

		if deleted < pruneBatch {
			break
		}
	}

	for {
		deleted, err := h.store.PruneDigest(ctx, now.Add(-DigestRetention), pruneBatch)
		if err != nil {
			return swept, fmt.Errorf("incident housekeeping: %w", err)
		}

		swept.Digest += deleted

		if deleted < pruneBatch {
			return swept, nil
		}
	}
}
