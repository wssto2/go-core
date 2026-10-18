//nolint:err113,exhaustruct,exhaustruct_v5,goconst // tests in the style of the package's others
package preferences_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity/preferences"
	"github.com/wssto2/go-core/validation"
)

// memory is a Store in a map.
type memory struct {
	mu   sync.Mutex
	rows map[int]json.RawMessage
}

func newMemory() *memory { return &memory{rows: map[int]json.RawMessage{}} }

func (m *memory) Get(_ context.Context, id int) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.rows[id], nil
}

func (m *memory) Set(_ context.Context, id int, prefs json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.rows[id] = prefs

	return nil
}

func fields(t *testing.T, err error) map[string][]validation.Failure {
	t.Helper()

	var invalid *validation.ValidationError

	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, 422, apperr.GetHTTPStatus(err))

	return invalid.Failures
}

func TestGetIsAnEmptyObjectWhenNothingIsStored(t *testing.T) {
	t.Parallel()

	svc, err := preferences.New(preferences.Config{Store: newMemory()})
	require.NoError(t, err)

	got, err := svc.Get(t.Context(), 1)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(got))
}

func TestUpdateMergesIntoWhatIsStoredAndAnswersTheWholeObject(t *testing.T) {
	t.Parallel()

	svc, err := preferences.New(preferences.Config{Store: newMemory()})
	require.NoError(t, err)

	got, err := svc.Update(t.Context(), 1, []byte(`{"dark_mode":true,"tips":["a"]}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"dark_mode":true,"tips":["a"]}`, string(got))

	got, err = svc.Update(t.Context(), 1, []byte(`{"tips":null,"lang":"hr"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"dark_mode":true,"lang":"hr"}`, string(got))

	stored, err := svc.Get(t.Context(), 1)
	require.NoError(t, err)
	assert.JSONEq(t, string(got), string(stored))

	other, err := svc.Get(t.Context(), 2)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(other), "another person's preferences are their own")
}

func TestUpdateRefusesAPatchThatIsNotAnObject(t *testing.T) {
	t.Parallel()

	svc, err := preferences.New(preferences.Config{Store: newMemory()})
	require.NoError(t, err)

	for _, patch := range []string{``, `null`, `[]`, `"x"`, `{"a":`} {
		_, err := svc.Update(t.Context(), 1, []byte(patch))
		assert.Equal(t, validation.CodeMustBeObject, fields(t, err)["preferences"][0].Code, patch)
	}
}

func TestTheStoredObjectIsCapped(t *testing.T) {
	t.Parallel()

	store := newMemory()
	svc, err := preferences.New(preferences.Config{Store: store, MaxBytes: 64})
	require.NoError(t, err)

	_, err = svc.Update(t.Context(), 1, []byte(`{"a":"`+strings.Repeat("x", 40)+`"}`))
	require.NoError(t, err)

	// each patch is small, the object they make is not
	_, err = svc.Update(t.Context(), 1, []byte(`{"b":"`+strings.Repeat("y", 40)+`"}`))
	failure := fields(t, err)["preferences"][0]
	assert.Equal(t, validation.CodeMax, failure.Code)
	assert.EqualValues(t, 64, failure.Params["max"])

	stored, _ := store.Get(t.Context(), 1)
	assert.NotContains(t, string(stored), `"b"`, "a refused patch writes nothing")

	// a patch wider than the cap is refused before anything is read
	_, err = svc.Update(t.Context(), 2, []byte(`{"a":"`+strings.Repeat("x", 100)+`"}`))
	assert.Equal(t, validation.CodeMax, fields(t, err)["preferences"][0].Code)
}

func TestTheDefaultCapIs16KiB(t *testing.T) {
	t.Parallel()

	svc, err := preferences.New(preferences.Config{Store: newMemory()})
	require.NoError(t, err)

	_, err = svc.Update(t.Context(), 1, []byte(`{"a":"`+strings.Repeat("x", preferences.DefaultMaxBytes-20)+`"}`))
	require.NoError(t, err)

	_, err = svc.Update(t.Context(), 1, []byte(`{"a":"`+strings.Repeat("x", preferences.DefaultMaxBytes)+`"}`))
	assert.Equal(t, validation.CodeMax, fields(t, err)["preferences"][0].Code)
}

func TestTheApplicationsMergeAndValidateReplaceTheDefaults(t *testing.T) {
	t.Parallel()

	store := newMemory()
	svc, err := preferences.New(preferences.Config{
		Store: store,
		// flags only turn on
		Merge: func(stored, patch json.RawMessage) (json.RawMessage, error) {
			var was, now map[string]bool

			_ = json.Unmarshal(stored, &was)
			_ = json.Unmarshal(patch, &now)

			if was == nil {
				was = map[string]bool{}
			}

			for k, v := range now {
				was[k] = was[k] || v
			}

			return json.Marshal(was)
		},
		Validate: func(merged json.RawMessage) error {
			if strings.Contains(string(merged), `"bad"`) {
				return validation.NewValidationError("invalid", map[string][]validation.Failure{
					"preferences.bad": {validation.Fail("identifier_format")},
				}, nil)
			}

			return nil
		},
	})
	require.NoError(t, err)

	_, err = svc.Update(t.Context(), 1, []byte(`{"welcome":true}`))
	require.NoError(t, err)

	got, err := svc.Update(t.Context(), 1, []byte(`{"welcome":false,"tips":true}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"welcome":true,"tips":true}`, string(got))

	_, err = svc.Update(t.Context(), 1, []byte(`{"bad":true}`))
	assert.Equal(t, validation.RuleCode("identifier_format"), fields(t, err)["preferences.bad"][0].Code)

	stored, _ := store.Get(t.Context(), 1)
	assert.JSONEq(t, `{"welcome":true,"tips":true}`, string(stored), "a refused update writes nothing")
}

func TestAValidateErrorThatIsNotAValidationErrorAnswers422(t *testing.T) {
	t.Parallel()

	svc, err := preferences.New(preferences.Config{
		Store:    newMemory(),
		Validate: func(json.RawMessage) error { return errors.New("no way") },
	})
	require.NoError(t, err)

	_, err = svc.Update(t.Context(), 1, []byte(`{"a":1}`))
	require.Len(t, fields(t, err)["preferences"], 1)
}

func TestAMergeThatReturnsAnythingButAnObjectIsAServerError(t *testing.T) {
	t.Parallel()

	svc, err := preferences.New(preferences.Config{
		Store: newMemory(),
		Merge: func(_, _ json.RawMessage) (json.RawMessage, error) { return []byte(`[1]`), nil },
	})
	require.NoError(t, err)

	_, err = svc.Update(t.Context(), 1, []byte(`{"a":1}`))
	assert.Equal(t, 500, apperr.GetHTTPStatus(err))
}

func TestNewNeedsAStore(t *testing.T) {
	t.Parallel()

	_, err := preferences.New(preferences.Config{})
	require.Error(t, err)
}
