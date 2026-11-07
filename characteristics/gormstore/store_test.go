//nolint:exhaustruct,exhaustruct_v5,paralleltest,varnamelen,lll,goconst,thelper // tests in the style of the package's others
package gormstore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/characteristics/core"
	"github.com/wssto2/go-core/characteristics/storetest"
	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/gorm"
)

// The conformance suite, once for each table shape a store has to serve. The legacy shapes need MySQL or MariaDB
// (their DDL is MySQL's); go-core's own tables also run on SQLite.
func TestStoreConformsOnGoCoreTables(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) { storetest.Run(t, goCore().harness(t, db)) })
}

func TestStoreConformsOnSiteBuilderTables(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) { storetest.Run(t, siteBuilder().harness(t, db)) }, dbtest.MySQL, dbtest.MariaDB)
}

func TestStoreConformsOnArvNextTables(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) { storetest.Run(t, arvNext().harness(t, db)) }, dbtest.MySQL, dbtest.MariaDB)
}

// The service checks what a person writes against the description before the store sees it.
func TestServiceValidatesWhatIsWritten(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		h := goCore().harness(t, db)
		store := h.New(t)
		svc := core.New(h.Tree, store, core.Config{Clock: fixedClock{}})

		top, err := svc.Create(t.Context(), 1, 1, 0, core.Input{Name: "Top", Meta: map[string]any{"a": 1}})
		require.NoError(t, err)

		mid, err := svc.Create(t.Context(), 1, 2, top.ID, core.Input{Name: "Mid", Values: map[string]any{"slug": "  mid  "}})
		require.NoError(t, err)
		require.Equal(t, "mid", mid.Values["slug"], "text is trimmed")

		for name, in := range map[string]core.Input{
			"empty name":          {Name: "  "},
			"name too long":       {Name: strings.Repeat("x", 256)},
			"unknown column":      {Name: "A", Values: map[string]any{"legacy_note": "x"}},
			"text too long":       {Name: "A", Values: map[string]any{"slug": strings.Repeat("x", 31)}},
			"text given a number": {Name: "A", Values: map[string]any{"slug": 5}},
			"language not listed": {Name: "A", Translations: map[string]string{"de": "A"}},
		} {
			_, err := svc.Update(t.Context(), 1, 2, mid.ID, in)
			require.Equal(t, 400, apperr.GetHTTPStatus(err), name)
		}

		_, err = svc.Update(t.Context(), 1, 1, top.ID, core.Input{Name: "Top", Values: map[string]any{"slug": "x"}})
		require.Equal(t, 400, apperr.GetHTTPStatus(err), "a column of another level")

		_, err = svc.Create(t.Context(), 1, 2, 0, core.Input{Name: "No parent"})
		require.Equal(t, 400, apperr.GetHTTPStatus(err))

		_, err = svc.Create(t.Context(), 1, 1, top.ID, core.Input{Name: "Root with a parent"})
		require.Equal(t, 400, apperr.GetHTTPStatus(err))

		// Empty text of a nullable column is NULL, not an empty string.
		_, err = svc.Update(t.Context(), 1, 2, mid.ID, core.Input{Name: "Mid", Values: map[string]any{"slug": ""}})
		require.NoError(t, err)
		require.Equal(t, "NULL", h.Raw(t, 2, mid.ID)["slug"])

		_, err = svc.SetActive(t.Context(), 1, 9, 1, true)
		require.Equal(t, 404, apperr.GetHTTPStatus(err))
	}, dbtest.SQLite)
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC) }
