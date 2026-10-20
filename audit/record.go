package audit

import (
	"context"
	"errors"
	"maps"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/tenancy"
)

// ErrNoActor is what Record returns for a context with neither a signed-in person nor a system marker:
// an entry is never written for actor 0 by accident.
var ErrNoActor = errors.New("audit: ctx has no signed-in person and is not marked as a system actor")

type systemKey struct{}

// AsSystem marks ctx as acting for the system (a job, a command, a worker) under the given name; Record
// then writes actor 0 with metadata {"actor": "system", "job": job}. A signed-in person in ctx wins.
// A context marked tenancy.WithAllTenants without a person (workers and commands get one) counts as
// the system too, with no job name.
func AsSystem(ctx context.Context, job string) context.Context {
	return context.WithValue(ctx, systemKey{}, job)
}

// RecordOption adjusts the entry Record writes.
type RecordOption func(*Entry)

// WithMetadata adds metadata to the entry (an existing key is replaced).
func WithMetadata(meta map[string]any) RecordOption {
	return func(entry *Entry) {
		if entry.Metadata == nil {
			entry.Metadata = map[string]any{}
		}

		maps.Copy(entry.Metadata, meta)
	}
}

// Recorder writes entries for the actor of a context. The actor comes from a function the application
// gives it (auth.ActorID is the usual one), because audit does not import auth.
type Recorder struct {
	repo  Repository
	actor func(ctx context.Context) (int, bool)
}

// NewRecorder makes a Recorder writing through repo; actor returns the id of the signed-in person in ctx.
func NewRecorder(repo Repository, actor func(ctx context.Context) (int, bool)) *Recorder {
	return &Recorder{repo: repo, actor: actor}
}

// Record writes one entry through the repository, in the caller's transaction, for the actor ctx
// carries: the signed-in person, or the system (AsSystem, or an all-tenants context without a
// person). ctx with neither fails with ErrNoActor. ChangedFields come from Diff when before and
// after are both given and of the same struct type. Masking is the repository's, as for Write.
func (r *Recorder) Record(
	ctx context.Context, entityType string, entityID int, action string, before, after any, opts ...RecordOption,
) error {
	actor, meta, err := r.actorOf(ctx)
	if err != nil {
		return err
	}

	entry := NewEntry(entityType, entityID, actor, action).WithBefore(before).WithAfter(after)
	if before != nil && after != nil {
		entry = entry.WithDiff(Diff(before, after))
	}

	WithMetadata(meta)(&entry)

	for _, opt := range opts {
		opt(&entry)
	}

	return r.repo.Write(ctx, entry) //nolint:wrapcheck // the repository wraps its own errors
}

// actorOf is the actor id and the metadata that mark a system actor.
func (r *Recorder) actorOf(ctx context.Context) (int, map[string]any, error) {
	if id, ok := r.actor(ctx); ok && id > 0 {
		return id, nil, nil
	}

	job, marked := ctx.Value(systemKey{}).(string)
	if !marked && !tenancy.AllTenantsFromContext(ctx) {
		return 0, nil, apperr.Wrap(ErrNoActor, "cannot record an audit entry", apperr.CodeInternal)
	}

	meta := map[string]any{"actor": "system"}
	if job != "" {
		meta["job"] = job
	}

	return 0, meta, nil
}
