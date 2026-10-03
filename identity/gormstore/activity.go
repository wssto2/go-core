package gormstore

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/identity/account"
	"gorm.io/gorm"
)

// ActivityLog implements account.ActivityLog over go-core's audit trail: what a
// person did is the audit_logs rows whose actor they are, newest first (served
// by the index on actor and time). Who was signed in as them at the time is read
// off their sessions: one opened by signing in as somebody covers the time from
// its opening to its last use.
type ActivityLog struct {
	db *gorm.DB
}

var _ account.ActivityLog = (*ActivityLog)(nil)

// NewActivityLog returns the activity log over db. The audit_logs and tokens
// tables are audit/migrations' and identity/migrations'.
func NewActivityLog(db *gorm.DB) *ActivityLog { return &ActivityLog{db: db} }

// likeEscaper escapes what LIKE reads as a pattern, for ESCAPE '!': the one form
// MySQL, MariaDB and SQLite all read the same.
var likeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

// match is the SQL condition for a record set and its arguments, false for none.
func match(s account.RecordSet) (string, []any) {
	var (
		parts []string
		args  []any
	)

	if len(s.Types) > 0 {
		parts, args = append(parts, "entity_type IN ?"), append(args, s.Types)
	}

	for _, p := range s.Prefixes {
		parts, args = append(parts, "entity_type LIKE ? ESCAPE '!'"), append(args, likeEscaper.Replace(p)+"%")
	}

	return strings.Join(parts, " OR "), args
}

// Activity implements account.ActivityLog.
func (l *ActivityLog) Activity(ctx context.Context, q account.ActivityQuery) ([]account.ActivityEntry, int, error) {
	db := dbOf(l.db, ctx).Model(&audit.AuditLog{}).Where("actor_id = ?", q.ActorID)

	if in, args := match(q.Within); in != "" {
		db = db.Where("("+in+")", args...)
	}

	if out, args := match(q.Outside); out != "" {
		// entity_type may be NULL: such a row is outside every set, and NOT NULL would drop it.
		db = db.Where("COALESCE(("+out+"), 0) = 0", args...)
	}

	if !q.From.IsZero() {
		db = db.Where("created_at >= ?", millis(q.From))
	}

	if !q.To.IsZero() {
		db = db.Where("created_at < ?", millis(q.To))
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []audit.AuditLog

	err := db.Select("id", "entity_type", "entity_id", "action", "created_at").
		Order("created_at DESC, id DESC").Offset(q.Offset).Limit(q.Limit).Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, int(total), err
	}

	windows, err := l.signedInAs(ctx, q.ActorID)
	if err != nil {
		return nil, 0, err
	}

	out := make([]account.ActivityEntry, len(rows))
	for i, r := range rows {
		at := r.CreatedAt.UTC()
		out[i] = account.ActivityEntry{
			ID: int(r.ID), RecordType: r.EntityType, RecordID: r.EntityID, Action: account.ActivityActionOf(r.Action),
			SignedInAs: windows.at(at), At: at,
		}
	}

	return out, int(total), nil
}

// window is a time somebody was signed in as the person.
type window struct {
	actor      int
	start, end time.Time
}

type windows []window

// at is who was signed in at t, the session opened last first; zero for nobody.
func (w windows) at(t time.Time) int {
	for _, one := range slices.Backward(w) {
		if !t.Before(one.start) && !t.After(one.end) {
			return one.actor
		}
	}

	return 0
}

// lastUseSlack is how long after its last recorded use a session still counts: a
// session's use is written once a minute at most.
const lastUseSlack = time.Minute

// signedInAs lists the sessions opened by signing in as the person, oldest first.
// A session ends where it was last used (a revoked one has no other end on record),
// or where it expires.
func (l *ActivityLog) signedInAs(ctx context.Context, accountID int) (windows, error) {
	var tokens []auth.Token

	err := dbOf(l.db, ctx).Where("user_id = ? AND name LIKE ? ESCAPE '!'", accountID, likeEscaper.Replace(impersonationPrefix)+"%").
		Order("created_at, id").Find(&tokens).Error
	if err != nil {
		return nil, err
	}

	var out windows

	for _, t := range tokens {
		actor, _ := partsOf(t.Name)
		if actor == 0 {
			continue
		}

		start := t.CreatedAt.UTC()
		end := t.LastUsedAt.UTC()

		if end.Before(start) {
			end = start
		}

		out = append(out, window{actor: actor, start: start, end: minTime(t.ExpiresAt.UTC(), end.Add(lastUseSlack))})
	}

	return out, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
}
