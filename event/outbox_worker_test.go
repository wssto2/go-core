package event

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewOutboxWorker_NilLogger_UsesDefault confirms that passing a nil logger
// does not panic and that the stored logger is non-nil (slog.Default()).
func TestNewOutboxWorker_NilLogger_UsesDefault(t *testing.T) {
	var w *OutboxWorker
	require.NotPanics(t, func() {
		w = NewOutboxWorker(nil, nil, nil, time.Second, 10)
	})
	assert.NotNil(t, w.log, "logger must be non-nil when nil is passed to NewOutboxWorker")
}

// TestOutboxWorker_EmptyEventType_MarkedProcessed verifies that an outbox event
// with an empty EventType is dead-lettered (marked processed) so it is not
// re-fetched and re-skipped on every subsequent poll.
func TestOutboxWorker_EmptyEventType_MarkedProcessed(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, EnsureOutboxSchema(db))

	// Insert an outbox event with no EventType.
	e := &OutboxEvent{EventType: "", Envelope: []byte(`{}`)}
	require.NoError(t, db.Create(e).Error)

	published := 0
	publish := func(_ context.Context, _ string, _ []byte) error {
		published++
		return nil
	}

	w := NewOutboxWorker(db, publish, slog.Default(), time.Hour, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Run(ctx) //nolint:errcheck

	// The publish function must not have been called.
	assert.Equal(t, 0, published, "publish must not be called for empty-type events")

	// The event must now be marked processed (ProcessedAt != nil).
	var stored OutboxEvent
	require.NoError(t, db.First(&stored, e.ID).Error)
	assert.NotNil(t, stored.ProcessedAt, "event with empty type must be marked processed")
}

