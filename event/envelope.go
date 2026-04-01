package event

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/wssto2/go-core/observability/tracing"
)

// Envelope wraps application events with tracing and metadata fields required by
// the system: request_id, timestamp and source.
type Envelope struct {
	Version   string          `json:"_v"`
	RequestID string          `json:"request_id,omitempty"`
	Timestamp time.Time       `json:"timestamp,omitempty"`
	Source    string          `json:"source,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

const DefaultEventSource = "go-core"

// WrapEventWithMetadata serializes the provided event and wraps it in an Envelope
// populated with request id (from ctx or generated), timestamp and source.
func WrapEventWithMetadata(ctx context.Context, event any) (*Envelope, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	traceID, ok := tracing.TraceIDFromContext(ctx)
	if !ok || traceID == "" {
		traceID = uuid.NewString()
	}
	return &Envelope{
		Version:   "1",
		RequestID: traceID,
		Timestamp: time.Now().UTC(),
		Source:    DefaultEventSource,
		Payload:   raw,
	}, nil
}
