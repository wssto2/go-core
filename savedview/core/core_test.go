//nolint:err113,exhaustruct,exhaustruct_v5,paralleltest,varnamelen // tests in the style of the package's others
package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/savedview/core"
)

// fakeStore keeps views in a slice, scoped to the person as the real store is.
type fakeStore struct {
	rows   []core.View
	nextID int
	fail   error
}

func (f *fakeStore) List(_ context.Context, userID int, listKey string) ([]core.View, error) {
	var out []core.View

	for _, row := range f.rows {
		if row.UserID == userID && row.ListKey == listKey {
			out = append(out, row)
		}
	}

	return out, f.fail
}

func (f *fakeStore) FindByName(_ context.Context, userID int, listKey, name string) (core.View, error) {
	for _, row := range f.rows {
		if row.UserID == userID && row.ListKey == listKey && row.Name == name {
			return row, f.fail
		}
	}

	return core.View{}, core.ErrNotFound
}

func (f *fakeStore) Count(ctx context.Context, userID int, listKey string) (int, error) {
	rows, _ := f.List(ctx, userID, listKey)

	return len(rows), f.fail
}

func (f *fakeStore) Save(_ context.Context, view *core.View) error {
	if f.fail != nil {
		return f.fail
	}

	if view.ID == 0 {
		f.nextID++
		view.ID = f.nextID
		f.rows = append(f.rows, *view)

		return nil
	}

	for i, row := range f.rows {
		if row.ID == view.ID && row.UserID == view.UserID {
			f.rows[i] = *view

			return nil
		}
	}

	return core.ErrNotFound
}

func (f *fakeStore) Delete(_ context.Context, userID, id int) error {
	if f.fail != nil {
		return f.fail
	}

	for i, row := range f.rows {
		if row.ID == id && row.UserID == userID {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)

			return nil
		}
	}

	return core.ErrNotFound
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 9, 13, 10, 0, 0, 999, time.FixedZone("CET", 3600))
}

const list = "vehicles.catalogue"

func TestSaveReplacesTheSameName(t *testing.T) {
	store := &fakeStore{}
	svc := core.New(store, fixedClock{})

	first, err := svc.Save(t.Context(), 7, list, "New Clio", `{"filters":{"type":"new"}}`)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC), first.UpdatedAt, "UTC to the second")

	second, err := svc.Save(t.Context(), 7, list, "New Clio", `{"filters":{"type":"new","brand":"10"}}`)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	require.Len(t, store.rows, 1)
	assert.Contains(t, store.rows[0].State, "brand")
}

func TestSaveCapsNewNamesPerList(t *testing.T) {
	store := &fakeStore{}
	svc := core.New(store, fixedClock{})

	for i := range core.MaxPerList {
		_, err := svc.Save(t.Context(), 7, list, strings.Repeat("a", i+1), `{}`)
		require.NoError(t, err)
	}

	_, err := svc.Save(t.Context(), 7, list, "one too many", `{}`)
	require.Error(t, err)
	assert.True(t, apperr.HasCode(err, apperr.CodeBadRequest))
	assert.Contains(t, err.Error(), "at most 20 saved views per list")

	// Replacing an existing name is still allowed at the cap; another list and another person have room.
	_, err = svc.Save(t.Context(), 7, list, "a", `{"search":"x"}`)
	require.NoError(t, err)
	_, err = svc.Save(t.Context(), 7, "orders.list", "one", `{}`)
	require.NoError(t, err)
	_, err = svc.Save(t.Context(), 8, list, "one", `{}`)
	require.NoError(t, err)
}

func TestDeleteOfAnotherPersonsViewIsNotFound(t *testing.T) {
	store := &fakeStore{}
	svc := core.New(store, fixedClock{})

	saved, err := svc.Save(t.Context(), 7, list, "Mine", `{}`)
	require.NoError(t, err)

	err = svc.Delete(t.Context(), 8, saved.ID)
	assert.True(t, apperr.HasCode(err, apperr.CodeNotFound))
	require.Len(t, store.rows, 1)

	require.NoError(t, svc.Delete(t.Context(), 7, saved.ID))
	assert.Empty(t, store.rows)
}

func TestAStoreFailureIsAnInternalError(t *testing.T) {
	svc := core.New(&fakeStore{fail: errors.New("database is away")}, fixedClock{})

	_, err := svc.List(t.Context(), 7, list)
	assert.True(t, apperr.HasCode(err, apperr.CodeInternal))

	_, err = svc.Save(t.Context(), 7, list, "x", `{}`)
	assert.True(t, apperr.HasCode(err, apperr.CodeInternal))

	assert.True(t, apperr.HasCode(svc.Delete(t.Context(), 7, 1), apperr.CodeInternal))
}
