package event

import "context"

// ProcessedStore stores processed/reserved event IDs to allow idempotent processing.
type ProcessedStore interface {
	// Reserve attempts to reserve the given id. Returns true if reservation was
	// successful (id was not previously reserved/processed). Returns false when
	// the id was already reserved/processed.
	Reserve(ctx context.Context, id string) (bool, error)
	// Confirm marks the id as successfully processed.
	Confirm(ctx context.Context, id string) error
	// Release removes a reservation (used when processing fails and the id
	// should be retried later).
	Release(ctx context.Context, id string) error
}
