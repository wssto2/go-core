package notification

import (
	"errors"
	"time"
)

const minutesPerDay = 24 * 60

// QuietHours are when a person's e-mail waits (NOTIF-QUIET-001). Start and End are minutes after midnight on
// the wall clock of the application's TimeZone; End before Start means the window runs over midnight. The
// default for everyone, until they change it, is on, 21:00 to 07:00.
type QuietHours struct {
	Enabled bool `json:"enabled"`
	// Start is when quiet hours begin, in minutes after midnight (1260 is 21:00).
	Start int `json:"start"`
	// End is when they end, in minutes after midnight (420 is 07:00); it may be before Start.
	End int `json:"end"`
}

// QuietHoursInput is "set my quiet hours": the same three values, validated as the request comes in.
type QuietHoursInput struct {
	Enabled bool `json:"enabled"`
	Start   int  `json:"start" validation:"min:0|max:1439"`
	End     int  `json:"end" validation:"min:0|max:1439"`
}

// DefaultQuietHours is the default for everyone: on, 21:00 to 07:00.
func DefaultQuietHours() QuietHours {
	return QuietHours{Enabled: true, Start: 21 * 60, End: 7 * 60}
}

// ErrInvalidQuietHours means the quiet hours are out of range or empty (start equals end).
var ErrInvalidQuietHours = errors.New("notification: quiet hours need a start and an end between 00:00 and 23:59, and they must differ")

// Validate checks the bounds; an empty window (start equal to end) is refused.
func (q QuietHours) Validate() error {
	if q.Start < 0 || q.Start >= minutesPerDay || q.End < 0 || q.End >= minutesPerDay || q.Start == q.End {
		return ErrInvalidQuietHours
	}

	return nil
}

// ReleaseAt is when an e-mail made at now may go out: now itself outside quiet hours, or the end of the
// current quiet window, the next End on the wall clock of loc. The wall clock is read in loc, so a
// daylight-saving night still ends at the hour the person chose (NOTIF-QUIET-001, rules 1 and 2). Quiet hours
// that are off or invalid hold nothing.
func (q QuietHours) ReleaseAt(now time.Time, loc *time.Location) time.Time {
	if !q.Enabled || q.Validate() != nil {
		return now
	}

	local := now.In(loc)
	if !q.contains(local.Hour()*60 + local.Minute()) {
		return now
	}

	end := time.Date(local.Year(), local.Month(), local.Day(), q.End/60, q.End%60, 0, 0, loc)
	if !end.After(local) {
		end = time.Date(local.Year(), local.Month(), local.Day()+1, q.End/60, q.End%60, 0, 0, loc)
	}

	return end
}

func (q QuietHours) contains(minute int) bool {
	if q.Start < q.End {
		return minute >= q.Start && minute < q.End
	}

	return minute >= q.Start || minute < q.End
}
