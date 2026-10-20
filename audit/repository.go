package audit

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
)

// Repository defines the minimal audit persistence API.
type Repository interface {
	Write(ctx context.Context, entry Entry) error
}

// Entry represents a logical audit event to persist.
type Entry struct {
	EntityType    string
	EntityID      int
	Action        string
	ActorID       int
	Metadata      map[string]any
	BeforeState   any // will be marshalled to JSON
	AfterState    any
	ChangedFields []string
}

func NewEntry(entityType string, entityID, actorID int, action string) Entry {
	return Entry{EntityType: entityType, EntityID: entityID, Action: action, ActorID: actorID}
}

func (e Entry) WithBefore(v any) Entry         { e.BeforeState = v; return e }
func (e Entry) WithAfter(v any) Entry          { e.AfterState = v; return e }
func (e Entry) WithDiff(fields []string) Entry { e.ChangedFields = fields; return e }

// transactorRepository persists audit entries in a transaction on its own connection db: it joins the one ctx
// carries for db, else opens one, and ignores a transaction on another connection.
type transactorRepository struct {
	db       *gorm.DB
	tx       database.Transactor
	execHook func(ctx context.Context, log AuditLog) error // optional testing hook
}

// NewRepository constructs a Repository that writes on db.
func NewRepository(db *gorm.DB) Repository { //nolint:ireturn,lll // the port
	return &transactorRepository{db: db, tx: database.NewTransactor(db), execHook: nil}
}

// NewRepositoryWithHook constructs a Repository and installs a test hook used in unit tests.
func NewRepositoryWithHook(db *gorm.DB, hook func(ctx context.Context, log AuditLog) error) Repository { //nolint:ireturn,lll // the port
	return &transactorRepository{db: db, tx: database.NewTransactor(db), execHook: hook}
}

func (r *transactorRepository) Write(ctx context.Context, entry Entry) error {
	// Strip audit:"-" fields, then apply sensitive-field masking, then marshal.
	before, err := marshal(Mask(stripAuditFields(reflect.ValueOf(entry.BeforeState))))
	if err != nil {
		return apperr.Wrap(err, "failed to marshal before state", apperr.CodeInternal)
	}
	after, err := marshal(Mask(stripAuditFields(reflect.ValueOf(entry.AfterState))))
	if err != nil {
		return apperr.Wrap(err, "failed to marshal after state", apperr.CodeInternal)
	}
	fields, err := json.Marshal(entry.ChangedFields)
	if err != nil {
		return apperr.Wrap(err, "failed to marshal changed fields", apperr.CodeInternal)
	}

	log := AuditLog{
		EntityType:    entry.EntityType,
		EntityID:      entry.EntityID,
		Action:        entry.Action,
		ActorID:       entry.ActorID,
		Metadata:      toJSON(Mask(entry.Metadata)),
		BeforeState:   before,
		AfterState:    after,
		ChangedFields: fields,
		CreatedAt:     time.Now(),
	}

	// Persist inside a transaction
	return r.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		// If a test hook is provided, call it and return
		if r.execHook != nil {
			return r.execHook(txCtx, log)
		}

		// Prefer a *sql.Tx if the transactor stored one in the context
		if sqlTx, ok := database.SQLTxFor(txCtx, r.db); ok && sqlTx != nil {
			const insertSQL = `INSERT INTO audit_logs (entity_type, entity_id, action, actor_id, metadata, before_state, after_state, changed_fields, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
			if _, err := sqlTx.Exec(insertSQL,
				log.EntityType,
				log.EntityID,
				log.Action,
				log.ActorID,
				log.Metadata,
				log.BeforeState,
				log.AfterState,
				log.ChangedFields,
				log.CreatedAt,
			); err != nil {
				return apperr.Wrap(err, "failed to write audit log", apperr.CodeInternal)
			}
			return nil
		}

		// No sql.Tx available — fail deterministically.
		return apperr.Wrap(errors.New("no sql tx available"), "failed to write audit log", apperr.CodeInternal)
	})
}

// toJSON marshals v to JSON bytes and returns them. Errors are ignored on purpose
// because the data is not critical for the minimal persistence path.
func toJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
