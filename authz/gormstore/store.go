// Package gormstore is an authz.Store on GORM: custom roles, their grants and
// bindings in the roles, role_permissions and role_bindings tables. Every write
// runs in a transaction and, when built WithAudit, writes an audit row with the
// before and after state inside that same transaction, so a change and its
// record stand or fall together.
package gormstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Audit entity types written by the store.
const (
	EntityRole    = "authz.role"
	EntityBinding = "authz.binding"
)

// Store implements authz.Store on a *gorm.DB.
type Store struct {
	db    *gorm.DB
	tx    database.Transactor
	audit audit.Repository
	now   func() time.Time
}

var (
	_ authz.Store       = (*Store)(nil)
	_ authz.HolderStore = (*Store)(nil)
)

// Option configures a Store.
type Option func(*Store)

// WithAudit writes an audit row for every role and binding change, in the same
// transaction as the change. The audit repository should be built on a
// Transactor over the same database (audit.NewRepository).
func WithAudit(repo audit.Repository) Option { return func(s *Store) { s.audit = repo } }

// New returns a store on db. Call Migrate first.
func New(db *gorm.DB, opts ...Option) *Store {
	s := &Store{db: db, tx: database.NewTransactor(db), now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// reader is the connection reads use: the caller's transaction when the context
// carries one (so a read sees what the transaction wrote, and on a single
// connection does not wait for it), the database otherwise.
func (s *Store) reader(ctx context.Context) *gorm.DB {
	if tx, ok := database.TxFromContext(ctx); ok {
		return tx.WithContext(ctx)
	}
	return s.db.WithContext(ctx)
}

// BindingsFor implements authz.Reader.
func (s *Store) BindingsFor(ctx context.Context, sub authz.Subject) ([]authz.Binding, error) {
	var rows []roleBindingModel
	err := s.reader(ctx).Where("subject_kind = ? AND subject_id = ?", string(sub.Kind), sub.ID).
		Order("id").Find(&rows).Error
	if err != nil {
		return nil, apperr.Wrap(err, "load bindings", apperr.CodeInternal)
	}
	return bindingsFrom(rows), nil
}

// Role implements authz.Reader.
func (s *Store) Role(ctx context.Context, id int) (authz.Role, error) {
	return loadRole(s.reader(ctx), id)
}

// ListRoles implements authz.Store.
func (s *Store) ListRoles(ctx context.Context) ([]authz.Role, error) {
	db := s.reader(ctx)
	var rows []roleModel
	if err := db.Order("id").Find(&rows).Error; err != nil {
		return nil, apperr.Wrap(err, "list roles", apperr.CodeInternal)
	}
	var perms []rolePermissionModel
	if err := db.Order("role_id, permission").Find(&perms).Error; err != nil {
		return nil, apperr.Wrap(err, "list role permissions", apperr.CodeInternal)
	}
	byRole := map[int][]rolePermissionModel{}
	for _, p := range perms {
		byRole[p.RoleID] = append(byRole[p.RoleID], p)
	}
	out := make([]authz.Role, 0, len(rows))
	for _, r := range rows {
		role, err := toRole(r, byRole[r.ID])
		if err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, nil
}

// SaveRole implements authz.Store.
func (s *Store) SaveRole(ctx context.Context, actor authz.Subject, role authz.Role) (authz.Role, error) {
	var saved authz.Role
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		tx, err := txFrom(ctx)
		if err != nil {
			return err
		}
		attrs, err := marshalAttrs(role.Attrs)
		if err != nil {
			return err
		}
		action, id, before := "create", 0, (*authz.Role)(nil)
		if role.ID == 0 {
			id, err = s.insertRole(tx, actor, role, attrs)
		} else {
			action, id = "update", role.ID
			before, err = s.replaceRole(tx, actor, role, attrs)
		}
		if err != nil {
			return err
		}
		if err := insertGrants(tx, id, role.Grants); err != nil {
			return err
		}
		if saved, err = loadRole(tx, id); err != nil {
			return err
		}
		affected, err := affectedSubjects(tx, id)
		if err != nil {
			return err
		}
		return s.record(ctx, actor, EntityRole, id, action, snapshotRole(before), snapshotRole(&saved), map[string]any{"affected": affected})
	})
	if err != nil {
		return authz.Role{}, err
	}
	return saved, nil
}

func (s *Store) insertRole(tx *gorm.DB, actor authz.Subject, role authz.Role, attrs string) (int, error) {
	row := roleModel{Name: role.Name, Description: role.Description, Attrs: attrs, CreatedBy: actor.ID, UpdatedBy: actor.ID}
	if err := tx.Create(&row).Error; err != nil {
		return 0, apperr.Wrap(err, "create role", apperr.CodeInternal)
	}
	return row.ID, nil
}

// replaceRole updates the role's row and drops its grants; the caller inserts
// the new ones. It returns the role as it was.
func (s *Store) replaceRole(tx *gorm.DB, actor authz.Subject, role authz.Role, attrs string) (*authz.Role, error) {
	existing, err := loadRoleForUpdate(tx, role.ID)
	if err != nil {
		return nil, err
	}
	err = tx.Model(&roleModel{}).Where("id = ?", role.ID).Updates(map[string]any{
		"name": role.Name, "description": role.Description, "attrs": attrs,
		"updated_by": actor.ID, "updated_at": s.now(),
	}).Error
	if err != nil {
		return nil, apperr.Wrap(err, "update role", apperr.CodeInternal)
	}
	if err := tx.Where("role_id = ?", role.ID).Delete(&rolePermissionModel{}).Error; err != nil {
		return nil, apperr.Wrap(err, "replace role permissions", apperr.CodeInternal)
	}
	return &existing, nil
}

func insertGrants(tx *gorm.DB, roleID int, grants []authz.Grant) error {
	if len(grants) == 0 {
		return nil
	}
	perms := make([]rolePermissionModel, len(grants))
	for i, g := range grants {
		perms[i] = rolePermissionModel{RoleID: roleID, Permission: g.Permission, Qualifier: g.Qualifier.String()}
	}
	if err := tx.Create(&perms).Error; err != nil {
		return apperr.Wrap(err, "store role permissions", apperr.CodeInternal)
	}
	return nil
}

// DeleteRole implements authz.Store.
func (s *Store) DeleteRole(ctx context.Context, actor authz.Subject, id int) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		tx, err := txFrom(ctx)
		if err != nil {
			return err
		}
		existing, err := loadRoleForUpdate(tx, id)
		if err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&roleBindingModel{}).Where("role_id = ?", id).Count(&n).Error; err != nil {
			return apperr.Wrap(err, "count role bindings", apperr.CodeInternal)
		}
		if n > 0 {
			return fmt.Errorf("%w: id %d", authz.ErrRoleInUse, id)
		}
		if err := tx.Where("role_id = ?", id).Delete(&rolePermissionModel{}).Error; err != nil {
			return apperr.Wrap(err, "delete role permissions", apperr.CodeInternal)
		}
		if err := tx.Where("id = ?", id).Delete(&roleModel{}).Error; err != nil {
			return apperr.Wrap(err, "delete role", apperr.CodeInternal)
		}
		return s.record(ctx, actor, EntityRole, id, "delete", snapshotRole(&existing), nil, nil)
	})
}

// Binding implements authz.Store.
func (s *Store) Binding(ctx context.Context, id int) (authz.Binding, error) {
	return loadBinding(s.reader(ctx), id)
}

// BindingsForRole implements authz.Store.
func (s *Store) BindingsForRole(ctx context.Context, roleID int) ([]authz.Binding, error) {
	var rows []roleBindingModel
	if err := s.reader(ctx).Where("role_id = ?", roleID).Order("id").Find(&rows).Error; err != nil {
		return nil, apperr.Wrap(err, "load role bindings", apperr.CodeInternal)
	}
	return bindingsFrom(rows), nil
}

// HolderCounts implements authz.HolderStore. The distinct holders are counted
// here rather than in SQL: COUNT(DISTINCT a, b) is not portable to SQLite.
func (s *Store) HolderCounts(ctx context.Context) (authz.HolderCounts, error) {
	var rows []roleBindingModel
	err := s.reader(ctx).Model(&roleBindingModel{}).
		Select("DISTINCT role_id, role_key, subject_kind, subject_id").Find(&rows).Error
	if err != nil {
		return authz.HolderCounts{}, apperr.Wrap(err, "count role holders", apperr.CodeInternal)
	}
	out := authz.HolderCounts{ByID: map[int]int{}, ByKey: map[string]int{}}
	for _, r := range rows {
		if r.RoleID > 0 {
			out.ByID[r.RoleID]++
		} else {
			out.ByKey[r.RoleKey]++
		}
	}
	return out, nil
}

// HoldersOf implements authz.HolderStore.
func (s *Store) HoldersOf(ctx context.Context, ref authz.RoleRef) ([]authz.Binding, error) {
	if !ref.Valid() {
		return nil, nil
	}
	q := s.reader(ctx).Order("id")
	if ref.ID > 0 {
		q = q.Where("role_id = ?", ref.ID)
	} else {
		q = q.Where("role_id = 0 AND role_key = ?", ref.Key)
	}
	var rows []roleBindingModel
	if err := q.Find(&rows).Error; err != nil {
		return nil, apperr.Wrap(err, "load role holders", apperr.CodeInternal)
	}
	return bindingsFrom(rows), nil
}

// Bind implements authz.Store.
func (s *Store) Bind(ctx context.Context, actor authz.Subject, b authz.Binding) (authz.Binding, error) {
	var saved authz.Binding
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		tx, err := txFrom(ctx)
		if err != nil {
			return err
		}
		row := roleBindingModel{
			SubjectKind: string(b.Subject.Kind), SubjectID: b.Subject.ID,
			RoleID: b.Role.ID, RoleKey: b.Role.Key,
			ScopeLevel: b.Scope.Level, ScopeID: b.Scope.ID,
			CreatedBy: actor.ID, CreatedAt: s.now(),
		}
		var n int64
		err = tx.Model(&roleBindingModel{}).Where(
			"subject_kind = ? AND subject_id = ? AND role_id = ? AND role_key = ? AND scope_level = ? AND scope_id = ?",
			row.SubjectKind, row.SubjectID, row.RoleID, row.RoleKey, row.ScopeLevel, row.ScopeID).Count(&n).Error
		if err != nil {
			return apperr.Wrap(err, "check binding", apperr.CodeInternal)
		}
		if n > 0 {
			return authz.ErrDuplicateBinding
		}
		if err := tx.Create(&row).Error; err != nil {
			// a concurrent identical Bind may have committed after our pre-check; under
			// REPEATABLE READ we cannot see it, but the unique index refuses the insert
			if isDuplicateKey(err) {
				return authz.ErrDuplicateBinding
			}
			return apperr.Wrap(err, "create binding", apperr.CodeInternal)
		}
		saved = bindingFrom(row)
		return s.record(ctx, actor, EntityBinding, row.ID, "create", nil, snapshotBinding(saved), nil)
	})
	if err != nil {
		return authz.Binding{}, err
	}
	return saved, nil
}

// Unbind implements authz.Store.
func (s *Store) Unbind(ctx context.Context, actor authz.Subject, id int) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		tx, err := txFrom(ctx)
		if err != nil {
			return err
		}
		existing, err := loadBinding(tx, id)
		if err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&roleBindingModel{})
		if res.Error != nil {
			return apperr.Wrap(res.Error, "delete binding", apperr.CodeInternal)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: id %d", authz.ErrBindingNotFound, id)
		}
		return s.record(ctx, actor, EntityBinding, id, "delete", snapshotBinding(existing), nil, nil)
	})
}

// record writes the audit row, when auditing is on.
func (s *Store) record(ctx context.Context, actor authz.Subject, entity string, id int, action string, before, after any, meta map[string]any) error {
	if s.audit == nil {
		return nil
	}
	e := audit.NewEntry(entity, id, actor.ID, action).WithBefore(before).WithAfter(after)
	e.Metadata = map[string]any{"actor_kind": string(actor.Kind)}
	maps.Copy(e.Metadata, meta)
	if err := s.audit.Write(ctx, e); err != nil {
		return apperr.Wrap(err, "audit authz change", apperr.CodeInternal)
	}
	return nil
}

func txFrom(ctx context.Context) (*gorm.DB, error) {
	tx, ok := database.TxFromContext(ctx)
	if !ok {
		return nil, apperr.Internal(errors.New("authz/gormstore: no transaction in context"))
	}
	return tx, nil
}

// loadRoleForUpdate reads the role's row under a write lock (a no-op on SQLite).
func loadRoleForUpdate(tx *gorm.DB, id int) (authz.Role, error) {
	var row roleModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authz.Role{}, fmt.Errorf("%w: id %d", authz.ErrRoleNotFound, id)
	}
	if err != nil {
		return authz.Role{}, apperr.Wrap(err, "load role", apperr.CodeInternal)
	}
	return loadRole(tx, row.ID)
}

func loadRole(db *gorm.DB, id int) (authz.Role, error) {
	var row roleModel
	if err := db.Where("id = ?", id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authz.Role{}, fmt.Errorf("%w: id %d", authz.ErrRoleNotFound, id)
		}
		return authz.Role{}, apperr.Wrap(err, "load role", apperr.CodeInternal)
	}
	var perms []rolePermissionModel
	if err := db.Where("role_id = ?", id).Order("permission").Find(&perms).Error; err != nil {
		return authz.Role{}, apperr.Wrap(err, "load role permissions", apperr.CodeInternal)
	}
	return toRole(row, perms)
}

func loadBinding(db *gorm.DB, id int) (authz.Binding, error) {
	var row roleBindingModel
	if err := db.Where("id = ?", id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authz.Binding{}, fmt.Errorf("%w: id %d", authz.ErrBindingNotFound, id)
		}
		return authz.Binding{}, apperr.Wrap(err, "load binding", apperr.CodeInternal)
	}
	return bindingFrom(row), nil
}

// affectedSubjects lists who holds the role, for the audit trail.
func affectedSubjects(tx *gorm.DB, roleID int) ([]string, error) {
	var rows []roleBindingModel
	if err := tx.Where("role_id = ?", roleID).Order("id").Find(&rows).Error; err != nil {
		return nil, apperr.Wrap(err, "load role holders", apperr.CodeInternal)
	}
	seen := map[string]bool{}
	out := []string{}
	for _, r := range rows {
		s := authz.Subject{Kind: authz.Kind(r.SubjectKind), ID: r.SubjectID}.String()
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out, nil
}

func toRole(row roleModel, perms []rolePermissionModel) (authz.Role, error) {
	role := authz.Role{ID: row.ID, Name: row.Name, Description: row.Description}
	for _, p := range perms {
		q, err := authz.ParseQualifier(p.Qualifier)
		if err != nil {
			return authz.Role{}, apperr.Wrap(fmt.Errorf("role %d, %s: %w", row.ID, p.Permission, err), "read role", apperr.CodeInternal)
		}
		role.Grants = append(role.Grants, authz.Grant{Permission: p.Permission, Qualifier: q})
	}
	if row.Attrs != "" {
		if err := json.Unmarshal([]byte(row.Attrs), &role.Attrs); err != nil {
			return authz.Role{}, apperr.Wrap(fmt.Errorf("role %d attrs: %w", row.ID, err), "read role", apperr.CodeInternal)
		}
	}
	return role, nil
}

func marshalAttrs(attrs map[string][]string) (string, error) {
	if len(attrs) == 0 {
		return "", nil
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		return "", apperr.Wrap(err, "encode role attributes", apperr.CodeInternal)
	}
	return string(b), nil
}

func bindingFrom(r roleBindingModel) authz.Binding {
	return authz.Binding{
		ID:        r.ID,
		Subject:   authz.Subject{Kind: authz.Kind(r.SubjectKind), ID: r.SubjectID},
		Role:      authz.RoleRef{ID: r.RoleID, Key: r.RoleKey},
		Scope:     authz.Scope{Level: r.ScopeLevel, ID: r.ScopeID},
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
	}
}

func bindingsFrom(rows []roleBindingModel) []authz.Binding {
	out := make([]authz.Binding, len(rows))
	for i, r := range rows {
		out[i] = bindingFrom(r)
	}
	return out
}

// roleSnapshot and bindingSnapshot are the audit before/after shapes.
type roleSnapshot struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Grants      []string            `json:"grants"`
	Attrs       map[string][]string `json:"attrs,omitempty"`
}

type bindingSnapshot struct {
	Subject string `json:"subject"`
	Role    string `json:"role"`
	Scope   string `json:"scope"`
}

func snapshotRole(r *authz.Role) any {
	if r == nil {
		return nil
	}
	grants := make([]string, len(r.Grants))
	for i, g := range r.Grants {
		grants[i] = g.Permission + "=" + g.Qualifier.String()
	}
	sort.Strings(grants)
	return roleSnapshot{Name: r.Name, Description: r.Description, Grants: grants, Attrs: r.Attrs}
}

func snapshotBinding(b authz.Binding) any {
	role := b.Role.Key
	if role == "" {
		role = fmt.Sprintf("#%d", b.Role.ID)
	}
	return bindingSnapshot{Subject: b.Subject.String(), Role: role, Scope: b.Scope.String()}
}

// isDuplicateKey recognises a unique-index violation without importing a
// database driver: gorm's translated error when the application enabled
// TranslateError, otherwise the message MySQL / MariaDB ("Duplicate entry") and
// SQLite ("UNIQUE constraint failed") give.
func isDuplicateKey(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "UNIQUE constraint failed")
}
