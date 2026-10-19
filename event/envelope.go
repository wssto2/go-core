package event

import (
	"encoding/json"
	"time"
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
