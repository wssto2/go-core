package incident

import (
	"net/url"
	"slices"
	"sync"
	"time"
)

// TrailEntry is one API call a person made. Page is the screen of the application it came from (the path of the
// Referer). The query string is left out: searches carry names and numbers.
type TrailEntry struct {
	At     time.Time `json:"at"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Page   string    `json:"page,omitempty"`
	Status int       `json:"status"`
	Ms     int64     `json:"ms"`
}

// trail keeps each caller's last calls in memory, so an incident can say what the person did before it (INC-TRAIL-001)
// without every request being written anywhere.
//
// shortcut: per process, lost on restart, and one slice per caller who has made a call since, which the user base
// bounds; add an idle eviction if one process ever serves many thousands of people.
type trail struct {
	size    int
	mu      sync.Mutex
	callers map[string][]TrailEntry
}

func newTrail(size int) *trail {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	return &trail{size: size, callers: map[string][]TrailEntry{}}
}

// recent are the caller's last calls, oldest first.
func (t *trail) recent(caller string) []TrailEntry {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return append([]TrailEntry(nil), t.callers[caller]...)
}

// add remembers a call and forgets the ones beyond the size.
func (t *trail) add(caller string, entry TrailEntry) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	entries := append(slices.Clone(t.callers[caller]), entry)
	if len(entries) > t.size {
		entries = append([]TrailEntry(nil), entries[len(entries)-t.size:]...)
	}

	t.callers[caller] = entries
}

// refererPath is the path of a Referer header.
func refererPath(referer string) string {
	u, err := url.Parse(referer)
	if err != nil {
		return ""
	}

	return u.Path
}
