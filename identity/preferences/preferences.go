// Package preferences is the per-person JSON object identity keeps for the client: dark mode, the tips a person
// dismissed, anything the interface wants to remember between devices. identity.WithPreferences installs it:
// PUT /v1/auth/preferences merges a partial object into the stored one, and the session payload carries the
// whole object as "preferences".
//
// The object is the application's, identity only keeps it. The rules are two functions the application may
// replace: Merge (default MergePatch, RFC 7396) and Validate (default none, for example "an onboarding flag only
// turns on"). The stored object is capped (DefaultMaxBytes). The store is a port: the default is identity's
// own table (identity/preferences/gormstore); an application that already has a column for it implements Store
// over that.
//
// Reading and writing is not atomic: two patches of one person that arrive together may overwrite each other's
// keys. A person's own client is the only writer, so this is accepted; a Store that needs more serialises in Set.
package preferences

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/validation"
)

// DefaultMaxBytes is the largest merged object accepted unless Config.MaxBytes says otherwise.
const DefaultMaxBytes = 16 << 10

// field is the name a failure is reported under.
const field = "preferences"

// Store keeps each account's preferences, by the account's id.
type Store interface {
	// Get returns the JSON object stored for the account, empty (nil) when nothing is.
	Get(ctx context.Context, accountID int) (json.RawMessage, error)
	// Set replaces the object stored for the account.
	Set(ctx context.Context, accountID int, prefs json.RawMessage) error
}

// Config is what identity.WithPreferences takes. The zero value is valid for identity: its own table, RFC 7396,
// no validation, 16 KiB.
type Config struct {
	// Store keeps the objects; identity.Install defaults it to identity's own table.
	Store Store
	// Merge applies the patch (a JSON object) to the stored object (empty when nothing is stored) and returns the
	// new object, which must be a JSON object. Default: MergePatch. Return a *validation.ValidationError to refuse
	// the patch with 422.
	Merge func(stored, patch json.RawMessage) (json.RawMessage, error)
	// Validate checks the merged object before it is stored. Default: none. Return a *validation.ValidationError
	// (fields named as you like, e.g. "preferences.onboarding.tips_dismissed") for 422; any other error answers
	// 422 as well, under the field "preferences".
	Validate func(merged json.RawMessage) error
	// MaxBytes is the largest merged object, and patch, accepted. Default: DefaultMaxBytes.
	MaxBytes int
}

// Service reads and updates preferences.
type Service struct {
	store    Store
	merge    func(stored, patch json.RawMessage) (json.RawMessage, error)
	validate func(merged json.RawMessage) error
	max      int
}

// errNoStore and errNotAnObject are programming errors of the application, reported to Run or as a 500.
var (
	errNoStore     = errors.New("preferences: a Store is needed")
	errNotAnObject = errors.New("preferences: Merge did not return a JSON object")
)

// New returns the service for the configuration; it needs a Store.
func New(cfg Config) (*Service, error) {
	if cfg.Store == nil {
		return nil, errNoStore
	}

	svc := &Service{store: cfg.Store, merge: cfg.Merge, validate: cfg.Validate, max: cfg.MaxBytes}
	if svc.merge == nil {
		svc.merge = MergePatch
	}

	if svc.max <= 0 {
		svc.max = DefaultMaxBytes
	}

	return svc, nil
}

// Get returns the account's object, {} when nothing is stored.
func (s *Service) Get(ctx context.Context, accountID int) (json.RawMessage, error) {
	raw, err := s.store.Get(ctx, accountID)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`), nil
	}

	return raw, nil
}

// Update (IAM-PREF-001) merges patch into the account's object, stores the result and returns it. A patch that is
// not a JSON object, a result over the cap and a result Validate refuses answer 422 and store nothing.
func (s *Service) Update(ctx context.Context, accountID int, patch json.RawMessage) (json.RawMessage, error) {
	if !isObject(patch) {
		return nil, refuse(validation.Fail(validation.CodeMustBeObject))
	}

	if len(patch) > s.max {
		return nil, s.tooBig()
	}

	stored, err := s.store.Get(ctx, accountID)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	merged, err := s.mergeAndCheck(stored, patch)
	if err != nil {
		return nil, err
	}

	err = s.store.Set(ctx, accountID, merged)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	return merged, nil
}

// mergeAndCheck is the application's Merge, then the cap and the application's Validate on its result.
func (s *Service) mergeAndCheck(stored, patch json.RawMessage) (json.RawMessage, error) {
	merged, err := s.merge(stored, patch)
	if err != nil {
		return nil, asApp(err)
	}

	if !isObject(merged) {
		return nil, apperr.Internal(errNotAnObject)
	}

	if len(merged) > s.max {
		return nil, s.tooBig()
	}

	if s.validate == nil {
		return merged, nil
	}

	err = s.validate(merged)
	if err == nil {
		return merged, nil
	}

	if _, isValidation := errors.AsType[*validation.ValidationError](err); isValidation {
		return nil, err
	}

	return nil, refuse(validation.Fail(validation.CodeInvalidType))
}

func (s *Service) tooBig() error {
	return refuse(validation.FailWith(validation.CodeMax, map[string]any{"max": s.max}))
}

// Invalid is the 422 a Validate or Merge hook returns for one field: the validation error go-core's other routes
// answer, with the field (any name, e.g. "preferences.onboarding.whats_new_seen") and the rule code.
func Invalid(field string, code validation.RuleCode) error {
	return validation.NewValidationError("validation failed",
		map[string][]validation.Failure{field: {validation.Fail(code)}}, map[string][]string{field: {string(code)}})
}

// refuse is the validation error (422) of one failure on the preferences field.
func refuse(f validation.Failure) error {
	return validation.NewValidationError("validation failed",
		map[string][]validation.Failure{field: {f}}, map[string][]string{field: {string(f.Code)}})
}

// asApp keeps an error the application's Merge made for a client (a validation error, an apperr), any other is a 500.
func asApp(err error) error {
	if _, isApp := errors.AsType[*apperr.AppError](err); isApp {
		return err
	}

	return apperr.Internal(err)
}

// isObject says whether raw is a JSON object.
func isObject(raw json.RawMessage) bool {
	value, err := decode(raw)
	if err != nil {
		return false
	}

	_, is := value.(map[string]any)

	return is
}
