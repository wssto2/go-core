package gormstore

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/identity/account"
	"gorm.io/gorm"
)

// ChangeLog implements account.ChangeLog over go-core's audit trail: a change
// is a row of audit_logs (entity "account" unless AuditRecord says otherwise), written in
// the transaction the context carries, so it commits with the change it records.
type ChangeLog struct {
	db      *gorm.DB
	audit   audit.Repository
	entity  string
	written map[account.ChangeAction]string // change action -> action name in the row
	read    map[string]account.ChangeAction // action name in the row -> change action
}

var _ account.ChangeLog = (*ChangeLog)(nil)

// ChangeLogOption adjusts NewChangeLog.
type ChangeLogOption func(*ChangeLog)

// AuditRecord keeps the history in audit_logs rows of the entity type, with the
// action names given for the change actions (an action not in the map keeps its own name).
// Rows already written under that type and those names are read as they are, so no
// row is rewritten. When several change actions share a name, a row is read back as
// the first of created, updated, deactivated, activated, password, email, profile
// that has it, and a view that filters by action is as exact as the names are distinct.
func AuditRecord(entityType string, actions map[account.ChangeAction]string) ChangeLogOption {
	return func(l *ChangeLog) {
		l.entity, l.written = entityType, actions
	}
}

// NewChangeLog returns the change log over db. The audit_logs table is
// audit/migrations'; identity.Install registers it.
func NewChangeLog(db *gorm.DB, opts ...ChangeLogOption) *ChangeLog {
	changes := &ChangeLog{
		db: db, audit: audit.NewRepository(db), entity: account.AccountRecord,
		written: nil, read: nil,
	}

	for _, opt := range opts {
		opt(changes)
	}

	changes.read = make(map[string]account.ChangeAction, len(changes.written))

	for _, a := range slices.Backward(canonicalActions()) { // the first canonical action wins
		if name, ok := changes.written[a]; ok {
			changes.read[name] = a
		}
	}

	return changes
}

// Record implements account.ChangeLog.
func (l *ChangeLog) Record(ctx context.Context, c account.Change) error {
	entry := audit.NewEntry(l.entity, c.AccountID, c.ActorID, l.name(c.Action)).WithDiff(c.Fields)
	if len(c.Before) > 0 {
		entry = entry.WithBefore(c.Before)
	}

	if len(c.After) > 0 {
		entry = entry.WithAfter(c.After)
	}

	return l.audit.Write(ctx, entry) //nolint:wrapcheck // the audit trail's error reaches the caller as it always did
}

// Changes implements account.ChangeLog.
func (l *ChangeLog) Changes(ctx context.Context, q account.ChangeQuery) ([]account.ChangeEntry, int, error) {
	where := func() *gorm.DB {
		scope := dbOf(l.db, ctx).Model(new(audit.AuditLog)).
			Where("entity_type = ? AND entity_id = ?", l.entity, q.AccountID)
		if len(q.Only) > 0 {
			scope = scope.Where("action IN ?", l.names(q.Only))
		}

		if len(q.Except) > 0 {
			scope = scope.Where("action NOT IN ?", l.names(q.Except))
		}

		return scope
	}

	var total int64
	if err := where().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []audit.AuditLog
	if err := where().Order("id DESC").Offset(q.Offset).Limit(q.Limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	out := make([]account.ChangeEntry, len(rows))
	for i, r := range rows {
		out[i] = account.ChangeEntry{
			ID: int(r.ID), At: r.CreatedAt.UTC(),
			Change: account.Change{
				AccountID: r.EntityID, ActorID: r.ActorID, Action: l.action(r.Action),
				Fields: fieldsOf(r.ChangedFields), Before: valuesOf(r.BeforeState), After: valuesOf(r.AfterState),
			},
		}
	}

	return out, int(total), nil
}

// ChangeCounts implements account.ChangeLog.
func (l *ChangeLog) ChangeCounts(ctx context.Context, accountID int) (map[account.ChangeAction]int, error) {
	var rows []struct {
		Action string
		N      int
	}

	err := dbOf(l.db, ctx).Model(&audit.AuditLog{}).Select("action, COUNT(*) AS n").
		Where("entity_type = ? AND entity_id = ?", l.entity, accountID).Group("action").Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make(map[account.ChangeAction]int, len(rows))
	for _, r := range rows {
		out[l.action(r.Action)] += r.N
	}

	return out, nil
}

func fieldsOf(raw json.RawMessage) []string {
	var fields []string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &fields) // a row this module did not write may hold anything: then no fields
	}

	return fields
}

func valuesOf(raw json.RawMessage) map[string]string {
	var values map[string]string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &values)
	}

	return values
}

// name is the action name rows of the change action carry.
func (l *ChangeLog) name(a account.ChangeAction) string {
	if n, ok := l.written[a]; ok {
		return n
	}

	return string(a)
}

// action is the change action a row's action name is read as.
func (l *ChangeLog) action(name string) account.ChangeAction {
	if a, ok := l.read[name]; ok {
		return a
	}

	return account.ChangeAction(name)
}

// names are the row action names that are read as one of the actions.
func (l *ChangeLog) names(actions []account.ChangeAction) []string {
	out := make([]string, 0, len(actions))

	for _, a := range actions {
		if n := l.name(a); l.action(n) == a {
			out = append(out, n)
		}
	}

	return out
}

// canonicalActions is the order in which a row name that several change actions share is read
// back as one of them: the first listed.
func canonicalActions() []account.ChangeAction {
	return []account.ChangeAction{
		account.ChangeCreated, account.ChangeUpdated, account.ChangeDeactivated, account.ChangeActivated,
		account.ChangePassword, account.ChangeEmail, account.ChangeProfile,
	}
}
