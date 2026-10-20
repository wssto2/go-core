//nolint:exhaustruct,exhaustruct_v5,lll,err113,wrapcheck,nlreturn,noinlineerr,paralleltest,varnamelen,wsl_v5
package audit_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeRepo struct{ entries []audit.Entry }

func (f *fakeRepo) Write(_ context.Context, e audit.Entry) error {
	f.entries = append(f.entries, e)
	return nil
}

type person struct{ ID int }

func (p person) GetID() int { return p.ID }

type doc struct {
	Name  string `json:"name"`
	Price int    `json:"price"`
}

func actorOf(ctx context.Context) (int, bool) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return 0, false
	}
	return u.GetID(), true
}

func rec(repo audit.Repository) *audit.Recorder { return audit.NewRecorder(repo, actorOf) }

func personCtx(id int) context.Context {
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	gc.Request = httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
	auth.SetUser(gc, person{ID: id})
	return gc.Request.Context()
}

func TestRecordActorFromAPersonWithDiff(t *testing.T) {
	repo := &fakeRepo{}
	if err := rec(repo).Record(personCtx(7), "Doc", 3, "update", doc{"a", 1}, doc{"a", 2}); err != nil {
		t.Fatal(err)
	}
	e := repo.entries[0]
	if e.ActorID != 7 || e.EntityType != "Doc" || e.EntityID != 3 || e.Action != "update" {
		t.Fatalf("entry: %+v", e)
	}
	if len(e.ChangedFields) != 1 || e.ChangedFields[0] != "price" || len(e.Metadata) != 0 {
		t.Fatalf("diff or metadata: %+v", e)
	}
}

func TestRecordSystemActorWithMetadata(t *testing.T) {
	repo := &fakeRepo{}
	ctx := audit.AsSystem(context.Background(), "vega-import")
	if err := rec(repo).Record(ctx, "Doc", 1, "create", nil, doc{"a", 1}, audit.WithMetadata(map[string]any{"run": 9})); err != nil {
		t.Fatal(err)
	}
	e := repo.entries[0]
	if e.ActorID != 0 || e.Metadata["actor"] != "system" || e.Metadata["job"] != "vega-import" || e.Metadata["run"] != 9 || e.ChangedFields != nil {
		t.Fatalf("entry: %+v", e)
	}
}

func TestRecordPersonBeatsSystem(t *testing.T) {
	repo := &fakeRepo{}
	if err := rec(repo).Record(audit.AsSystem(personCtx(4), "job"), "Doc", 1, "x", nil, nil); err != nil {
		t.Fatal(err)
	}
	if repo.entries[0].ActorID != 4 || repo.entries[0].Metadata["actor"] != nil {
		t.Fatalf("entry: %+v", repo.entries[0])
	}
}

func TestRecordWithoutAnActorFails(t *testing.T) {
	repo := &fakeRepo{}
	err := rec(repo).Record(context.Background(), "Doc", 1, "x", nil, nil)
	if !errors.Is(err, audit.ErrNoActor) || len(repo.entries) != 0 {
		t.Fatalf("err=%v entries=%d", err, len(repo.entries))
	}
}

func TestRecordJoinsTheCallersTransactionAndRollsBackWithIt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "a.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Migrate(db); err != nil {
		t.Fatal(err)
	}
	recorder := audit.NewRecorder(audit.NewRepository(db), actorOf)
	tx := database.NewTransactor(db)
	count := func() int64 {
		var n int64
		db.Model(&audit.AuditLog{}).Count(&n)
		return n
	}

	boom := errors.New("boom")
	err = tx.WithinTransaction(personCtx(1), func(ctx context.Context) error {
		if err := recorder.Record(ctx, "Doc", 1, "create", nil, doc{"a", 1}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || count() != 0 {
		t.Fatalf("a rolled-back change leaves no entry: err=%v n=%d", err, count())
	}

	err = tx.WithinTransaction(personCtx(1), func(ctx context.Context) error {
		return recorder.Record(ctx, "Doc", 1, "create", nil, doc{"a", 1})
	})
	if err != nil || count() != 1 {
		t.Fatalf("a committed change keeps its entry: err=%v n=%d", err, count())
	}
}
