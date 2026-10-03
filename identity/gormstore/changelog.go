package gormstore

import (
	"context"
	"encoding/json"

	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/identity/account"
	"gorm.io/gorm"
)

// entityType is how an account's rows are named in go-core's audit trail.
const entityType = "account"

// ChangeLog implements account.ChangeLog over go-core's audit trail: a change
// is a row of audit_logs (entity "account"), written in the transaction the
// context carries, so it commits with the change it records.
type ChangeLog struct {
	db    *gorm.DB
	audit audit.Repository
}

var _ account.ChangeLog = (*ChangeLog)(nil)

// NewChangeLog returns the change log over db. The audit_logs table is
// audit/migrations'; identity.Install registers it.
func NewChangeLog(db *gorm.DB) *ChangeLog {
	return &ChangeLog{db: db, audit: audit.NewRepository(database.NewTransactor(db))}
}

// Record implements account.ChangeLog.
func (l *ChangeLog) Record(ctx context.Context, c account.Change) error {
	e := audit.NewEntry(entityType, c.AccountID, c.ActorID, string(c.Action)).WithDiff(c.Fields)
	if len(c.Before) > 0 {
		e = e.WithBefore(c.Before)
	}

	if len(c.After) > 0 {
		e = e.WithAfter(c.After)
	}

	return l.audit.Write(ctx, e)
}

// Changes implements account.ChangeLog.
func (l *ChangeLog) Changes(ctx context.Context, accountID, offset, limit int) ([]account.ChangeEntry, int, error) {
	where := "entity_type = ? AND entity_id = ?"

	var total int64
	if err := dbOf(l.db, ctx).Model(&audit.AuditLog{}).Where(where, entityType, accountID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []audit.AuditLog
	if err := dbOf(l.db, ctx).Where(where, entityType, accountID).Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	out := make([]account.ChangeEntry, len(rows))
	for i, r := range rows {
		out[i] = account.ChangeEntry{
			ID: int(r.ID), At: r.CreatedAt.UTC(),
			Change: account.Change{
				AccountID: r.EntityID, ActorID: r.ActorID, Action: account.ChangeAction(r.Action),
				Fields: fieldsOf(r.ChangedFields), Before: valuesOf(r.BeforeState), After: valuesOf(r.AfterState),
			},
		}
	}

	return out, int(total), nil
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
