package account

import (
	"sync"
	"time"
)

const (
	attemptWindow = time.Minute
	// sweepAbove is the size of the table above which expired windows are dropped
	// on the next attempt.
	sweepAbove = 1024
)

// attempts counts the sign-in attempts of each login in fixed windows
// (IAM-USER-001 item 5). The lock (five wrong passwords) is recorded after each
// attempt, so attempts made at the same moment can all pass the check before
// the lock is written; at most limit attempts per login per window are let
// through, whatever the answers.
//
// ponytail: in memory, so per process: with several instances a person gets
// limit attempts per instance per minute, and the lock, read off the history, is
// the shared limit. A shared counter if the instances ever matter.
type attempts struct {
	mu      sync.Mutex
	clock   Clock
	limit   int
	windows map[string]window
}

type window struct {
	end   time.Time
	count int
}

func newAttempts(clock Clock, limit int) *attempts {
	return &attempts{clock: clock, limit: limit, windows: map[string]window{}}
}

// allow counts an attempt of login and reports whether it may go on; when it
// may not, until is the end of the window it exceeded.
func (a *attempts) allow(login string) (until time.Time, ok bool) {
	now := a.clock.Now()

	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.windows) > sweepAbove {
		for k, w := range a.windows {
			if !now.Before(w.end) {
				delete(a.windows, k)
			}
		}
	}

	w, found := a.windows[login]
	if !found || !now.Before(w.end) {
		w = window{end: now.Add(attemptWindow)}
	}

	w.count++
	a.windows[login] = w

	return w.end, w.count <= a.limit
}
