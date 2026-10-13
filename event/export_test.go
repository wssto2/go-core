package event

import (
	"context"
	"time"
)

// DueIDs is one poll of the queue: the ids due for each consumer, by consumer name.
func (q *Queue) DueIDs(ctx context.Context, now time.Time) (map[string][]uint64, error) {
	due, err := dueFor(ctx, q.db, q.consumers, now, batchSize)
	if err != nil {
		return nil, err
	}

	out := map[string][]uint64{}
	for i, ids := range due {
		out[q.consumers[i].name] = ids
	}

	return out, nil
}
